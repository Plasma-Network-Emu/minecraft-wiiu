package matchmake_extension

import (
	"github.com/PretendoNetwork/nex-go/v2"
	"github.com/PretendoNetwork/nex-go/v2/types"
	common_globals "github.com/PretendoNetwork/nex-protocols-common-go/v2/globals"
	"github.com/PretendoNetwork/nex-protocols-common-go/v2/matchmake-extension/database"
	matchmake_extension "github.com/PretendoNetwork/nex-protocols-go/v2/matchmake-extension"
)

func (commonProtocol *CommonProtocol) modifyCurrentGameAttribute(err error, packet nex.PacketInterface, callID uint32, gid types.UInt32, attribIndex types.UInt32, newValue types.UInt32) (*nex.RMCMessage, *nex.Error) {
	if err != nil {
		common_globals.Logger.Error(err.Error())
		return nil, nex.NewError(nex.ResultCodes.Core.InvalidArgument, "change_error")
	}

	connection := packet.Sender().(*nex.PRUDPConnection)
	endpoint := connection.Endpoint().(*nex.PRUDPEndPoint)

	commonProtocol.manager.Mutex.Lock()
	session, _, nexError := database.GetMatchmakeSessionByID(commonProtocol.manager, endpoint, uint32(gid))
	if nexError != nil {
		commonProtocol.manager.Mutex.Unlock()
		return nil, nexError
	}

	if !session.Gathering.OwnerPID.Equals(connection.PID()) {
		commonProtocol.manager.Mutex.Unlock()
		return nil, nex.NewError(nex.ResultCodes.RendezVous.PermissionDenied, "change_error")
	}

	// Minecraft Wii U's NEX wrapper sends matchmaking attribute indexes as
	// 1-based values: 1 = attributes[0], 2 = attributes[1], ... 6 = attributes[5].
	// The database helper expects a zero-based index and converts it to the
	// PostgreSQL array index internally.
	if uint32(attribIndex) == 0 {
		commonProtocol.manager.Mutex.Unlock()
		return nil, nex.NewError(nex.ResultCodes.Core.InvalidIndex, "change_error")
	}

	index := int(attribIndex) - 1
	if index < 0 || index >= len(session.Attributes) {
		commonProtocol.manager.Mutex.Unlock()
		return nil, nex.NewError(nex.ResultCodes.Core.InvalidIndex, "change_error")
	}

	nexError = database.UpdateGameAttribute(commonProtocol.manager, uint32(gid), uint32(index), uint32(newValue))
	if nexError != nil {
		commonProtocol.manager.Mutex.Unlock()
		return nil, nexError
	}

	commonProtocol.manager.Mutex.Unlock()

	rmcResponse := nex.NewRMCSuccess(endpoint, nil)
	rmcResponse.ProtocolID = matchmake_extension.ProtocolID
	rmcResponse.MethodID = matchmake_extension.MethodModifyCurrentGameAttribute
	rmcResponse.CallID = callID

	if commonProtocol.OnAfterModifyCurrentGameAttribute != nil {
		go commonProtocol.OnAfterModifyCurrentGameAttribute(packet, gid, attribIndex, newValue)
	}

	return rmcResponse, nil
}
