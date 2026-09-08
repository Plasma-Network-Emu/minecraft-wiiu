package nex

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PretendoNetwork/minecraft-wiiu/globals"
	"github.com/PretendoNetwork/nex-go/v2"
	"github.com/PretendoNetwork/nex-go/v2/types"
	match_making "github.com/PretendoNetwork/nex-protocols-go/v2/match-making"
	match_making_types "github.com/PretendoNetwork/nex-protocols-go/v2/match-making/types"
)

const ixoraPublicSignatureLow24 = uint32(0x030881)

var ixoraHTTPClient = &http.Client{
	Timeout: 3 * time.Second,
}

type ixoraOpenPayload struct {
	SessionID  string   `json:"sessionId"`
	GameMode   uint32   `json:"gameMode"`
	Attributes []uint32 `json:"attributes"`
}

type ixoraUpdatePayload struct {
	SessionID      string `json:"sessionId"`
	AvailableSlots uint32 `json:"availableSlots"`
}

type ixoraClosePayload struct {
	SessionID string `json:"sessionId"`
}

func ixoraConfigured() bool {
	return os.Getenv("PN_MINECRAFT_ALLOW_PUBLIC_MATCHMAKING") == "1" &&
		strings.TrimSpace(os.Getenv("PN_MINECRAFT_IXORA_URL")) != "" &&
		strings.TrimSpace(os.Getenv("PN_MINECRAFT_IXORA_API_KEY")) != ""
}

func ixoraPost(path string, payload any, ignoreNotFound bool) error {
	if !ixoraConfigured() {
		return nil
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("PN_MINECRAFT_IXORA_URL")), "/")
	req, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", os.Getenv("PN_MINECRAFT_IXORA_API_KEY"))

	resp, err := ixoraHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if ignoreNotFound && resp.StatusCode == http.StatusNotFound {
		io.Copy(io.Discard, resp.Body)
		return nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("Ixora returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	io.Copy(io.Discard, resp.Body)
	return nil
}

func ixoraAfterCreateMatchmakeSession(
	packet nex.PacketInterface,
	_ match_making_types.GatheringHolder,
	_ types.String,
	_ types.UInt16,
) {
	if !ixoraConfigured() {
		return
	}

	connection := packet.Sender().(*nex.PRUDPConnection)

	var (
		gatheringID int64
		gameMode    int64
		attr0       int64
		attr1       int64
		attr2       int64
		attr3       int64
		attr4       int64
		attr5       int64
	)

	err := globals.MatchmakingManager.Database.QueryRow(`
		SELECT
			g.id,
			m.game_mode,
			COALESCE(m.attribs[1], 0),
			COALESCE(m.attribs[2], 0),
			COALESCE(m.attribs[3], 0),
			COALESCE(m.attribs[4], 0),
			COALESCE(m.attribs[5], 0),
			COALESCE(m.attribs[6], 0)
		FROM matchmaking.gatherings g
		JOIN matchmaking.matchmake_sessions m ON m.id = g.id
		WHERE g.type = 'MatchmakeSession'
			AND g.owner_pid = $1
			AND g.registered = true
			AND m.game_mode IN (1, 2, 3)
			AND array_length(m.attribs, 1) >= 2
			AND ((m.attribs[1]::bigint & 16777215) = 198785)
		ORDER BY g.id DESC
		LIMIT 1
	`, uint64(connection.PID())).Scan(
		&gatheringID,
		&gameMode,
		&attr0,
		&attr1,
		&attr2,
		&attr3,
		&attr4,
		&attr5,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			// Private/friends-only sessions intentionally do not match this query.
			return
		}

		globals.Logger.Warningf("Ixora: failed to resolve newly-created public lobby: %v", err)
		return
	}

	attributes := []uint32{
		uint32(attr0),
		uint32(attr1),
		uint32(attr2),
		uint32(attr3),
		uint32(attr4),
		uint32(attr5),
	}

	if len(attributes) < 2 || attributes[1] == 0 {
		return
	}

	if attributes[0]&0xFFFFFF != ixoraPublicSignatureLow24 {
		return
	}

	payload := ixoraOpenPayload{
		SessionID:  strconv.FormatInt(gatheringID, 10),
		GameMode:   uint32(gameMode),
		Attributes: attributes,
	}

	if err := ixoraPost("/lobbies/open", payload, false); err != nil {
		globals.Logger.Warningf("Ixora: failed to announce public lobby %d: %v", gatheringID, err)
	}
}

func ixoraAfterModifyCurrentGameAttribute(
	_ nex.PacketInterface,
	gid types.UInt32,
	attribIndex types.UInt32,
	newValue types.UInt32,
) {
	if !ixoraConfigured() || uint32(attribIndex) != 1 {
		return
	}

	payload := ixoraUpdatePayload{
		SessionID:      strconv.FormatUint(uint64(gid), 10),
		AvailableSlots: uint32(newValue),
	}

	if err := ixoraPost("/lobbies/update", payload, true); err != nil {
		globals.Logger.Warningf("Ixora: failed to update lobby %d slots: %v", uint32(gid), err)
	}
}

func ixoraAfterUnregisterGathering(_ nex.PacketInterface, gid types.UInt32) {
	if !ixoraConfigured() {
		return
	}

	ixoraCloseLobby(uint32(gid))
}

func ixoraCloseLobby(gid uint32) {
	payload := ixoraClosePayload{
		SessionID: strconv.FormatUint(uint64(gid), 10),
	}

	if err := ixoraPost("/lobbies/close", payload, true); err != nil {
		globals.Logger.Warningf("Ixora: failed to close lobby %d: %v", gid, err)
	}
}

func ixoraHandleConnectionEnded(connection *nex.PRUDPConnection) {
	if !ixoraConfigured() {
		return
	}

	rows, err := globals.MatchmakingManager.Database.Query(`
		SELECT g.id, g.flags, cardinality(g.participants)
		FROM matchmaking.gatherings g
		JOIN matchmaking.matchmake_sessions m ON m.id = g.id
		WHERE g.type = 'MatchmakeSession'
			AND g.owner_pid = $1
			AND g.registered = true
			AND m.game_mode IN (1, 2, 3)
			AND array_length(m.attribs, 1) >= 2
			AND ((m.attribs[1]::bigint & 16777215) = 198785)
	`, uint64(connection.PID()))
	if err != nil {
		globals.Logger.Warningf("Ixora: failed to find public lobbies for disconnected owner %d: %v", uint64(connection.PID()), err)
		return
	}
	defer rows.Close()

	var lobbyIDs []uint32

	for rows.Next() {
		var (
			gatheringID      int64
			flags            int64
			participantCount int64
		)

		if err := rows.Scan(&gatheringID, &flags, &participantCount); err != nil {
			globals.Logger.Warningf("Ixora: failed to scan disconnected owner's lobby: %v", err)
			continue
		}

		changeOwner := uint32(flags)&match_making.GatheringFlags.DisconnectChangeOwner != 0
		if changeOwner && participantCount > 1 {
			// The gathering survives and ownership will migrate, so it is not closed.
			continue
		}

		lobbyIDs = append(lobbyIDs, uint32(gatheringID))
	}

	if err := rows.Err(); err != nil {
		globals.Logger.Warningf("Ixora: failed while reading disconnected owner's lobbies: %v", err)
		return
	}

	for _, gid := range lobbyIDs {
		go ixoraCloseLobby(gid)
	}
}
