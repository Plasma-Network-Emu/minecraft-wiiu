package database

import (
	"crypto/rand"

	"github.com/PretendoNetwork/nex-go/v2"
	"github.com/PretendoNetwork/nex-go/v2/types"
	common_globals "github.com/PretendoNetwork/nex-protocols-common-go/v2/globals"
	match_making_database "github.com/PretendoNetwork/nex-protocols-common-go/v2/match-making/database"
	match_making_types "github.com/PretendoNetwork/nex-protocols-go/v2/match-making/types"
	pqextended "github.com/PretendoNetwork/pq-extended"
)

// CreateMatchmakeSession creates a new MatchmakeSession on the database. No participants are added
func CreateMatchmakeSession(manager *common_globals.MatchmakingManager, connection *nex.PRUDPConnection, matchmakeSession *match_making_types.MatchmakeSession) *nex.Error {
	startedTime, nexError := match_making_database.RegisterGathering(manager, connection.PID(), connection.PID(), &matchmakeSession.Gathering, "MatchmakeSession")
	if nexError != nil {
		return nexError
	}

	attribs := make([]uint32, len(matchmakeSession.Attributes))
	for i, value := range matchmakeSession.Attributes {
		attribs[i] = uint32(value)
	}

	// Minecraft Wii U Edition does not set the "public" bit (0x10000) in
	// attribs[0] when hosting a public game, even though its own
	// BrowseMatchmakeSession search criteria and CanJoinMatchmakeSession
	// check both expect that bit to be set. Public games are consequently
	// invisible to matchmaking. This corrects the specific, confirmed
	// pattern without touching anything else.
	if len(attribs) > 0 && attribs[0]&0xFFFFFF == 0x20881 {
		attribs[0] |= 0x10000
	}

	endpoint := connection.Endpoint().(*nex.PRUDPEndPoint)

	matchmakeParam := nex.NewByteStreamOut(endpoint.LibraryVersions(), endpoint.ByteStreamSettings())

	srVariant := types.NewVariant()
	srVariant.TypeID = 3
	srVariant.Type = types.NewBool(true)
	matchmakeSession.MatchmakeParam.Params["@SR"] = srVariant
	girVariant := types.NewVariant()
	girVariant.TypeID = 1
	girVariant.Type = types.NewInt64(3)
	matchmakeSession.MatchmakeParam.Params["@GIR"] = girVariant

	matchmakeSession.MatchmakeParam.WriteTo(matchmakeParam)

	matchmakeSession.StartedTime = startedTime
	matchmakeSession.SessionKey = make([]byte, 32)
	matchmakeSession.SystemPasswordEnabled = false
	rand.Read(matchmakeSession.SessionKey)

	_, err := manager.Database.Exec(`INSERT INTO matchmaking.matchmake_sessions (
		id,
		game_mode,
		attribs,
		open_participation,
		matchmake_system_type,
		application_buffer,
		progress_score,
		session_key,
		option_zero,
		matchmake_param,
		user_password,
		refer_gid,
		user_password_enabled,
		system_password_enabled,
		codeword
	) VALUES (
		$1,
		$2,
		$3,
		$4,
		$5,
		$6,
		$7,
		$8,
		$9,
		$10,
		$11,
		$12,
		$13,
		$14,
		$15
	)`,
		matchmakeSession.Gathering.ID,
		matchmakeSession.GameMode,
		pqextended.Array(attribs),
		matchmakeSession.OpenParticipation,
		matchmakeSession.MatchmakeSystemType,
		matchmakeSession.ApplicationBuffer,
		matchmakeSession.ProgressScore,
		matchmakeSession.SessionKey,
		matchmakeSession.Option,
		matchmakeParam.Bytes(),
		matchmakeSession.UserPassword,
		matchmakeSession.ReferGID,
		matchmakeSession.UserPasswordEnabled,
		matchmakeSession.SystemPasswordEnabled,
		matchmakeSession.CodeWord,
	)
	if err != nil {
		return nex.NewError(nex.ResultCodes.Core.Unknown, err.Error())
	}

	return nil
}
