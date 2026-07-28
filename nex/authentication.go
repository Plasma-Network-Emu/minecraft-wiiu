package nex

import (
	"fmt"
	"net"
	"os"
	"strconv"

	"github.com/PretendoNetwork/minecraft-wiiu/globals"
	"github.com/PretendoNetwork/nex-go/v2"
)

func startHealthCheckResponder(port int) {
	address, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return
	}
	socket, err := net.ListenUDP("udp", address)
	if err != nil {
		return
	}
	buffer := make([]byte, 1024)
	for {
		n, client, err := socket.ReadFromUDP(buffer)
		if err == nil {
			socket.WriteToUDP(buffer[:n], client)
		}
	}
}

func StartAuthenticationServer() {
	if healthPort, err := strconv.Atoi(os.Getenv("PN_MINECRAFT_HEALTH_CHECK_PORT")); err == nil && healthPort != 0 {
		go startHealthCheckResponder(healthPort)
	}

	globals.AuthenticationServer = nex.NewPRUDPServer()
	globals.AuthenticationServer.ByteStreamSettings.UseStructureHeader = true

	globals.AuthenticationEndpoint = nex.NewPRUDPEndPoint(1)
	globals.AuthenticationEndpoint.ServerAccount = globals.AuthenticationServerAccount
	globals.AuthenticationEndpoint.AccountDetailsByPID = globals.AccountDetailsByPID
	globals.AuthenticationEndpoint.AccountDetailsByUsername = globals.AccountDetailsByUsername
	globals.AuthenticationServer.BindPRUDPEndPoint(globals.AuthenticationEndpoint)

	globals.AuthenticationServer.LibraryVersions.SetDefault(nex.NewLibraryVersion(3, 10, 0))
	globals.AuthenticationServer.AccessKey = "f1b61c8e"

	globals.AuthenticationEndpoint.OnData(func(packet nex.PacketInterface) {
		request := packet.RMCMessage()

		fmt.Println("==Minecraft: Wii U Edition - Auth==")
		fmt.Printf("Protocol ID: %#v\n", request.ProtocolID)
		fmt.Printf("Method ID: %#v\n", request.MethodID)
		fmt.Println("===============")
	})

	globals.AuthenticationEndpoint.OnError(func(err *nex.Error) {
		globals.Logger.Errorf("Auth: %v", err)
	})

	registerCommonAuthenticationServerProtocols()

	port, _ := strconv.Atoi(os.Getenv("PN_MINECRAFT_AUTHENTICATION_SERVER_PORT"))

	globals.AuthenticationServer.Listen(port)
}
