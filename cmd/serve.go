package cmd

import (
	"github.com/asubiotto/gupload/core"
	"gopkg.in/urfave/cli.v2"
)

var Serve = cli.Command{
	Name:   "serve",
	Usage:  "initiates a gRPC server",
	Action: serveAction,
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "port",
			Usage: "port to bind to",
			Value: 1313,
		},
		&cli.BoolFlag{
			Name:  "dwindow",
			Usage: "whether to use GRPC dynamic window resizing",
		},
	},
}

func serveAction(c *cli.Context) (err error) {
	var (
		port        = c.Int("port")
		key         = c.String("key")
		certificate = c.String("certificate")
		dwindow     = c.Bool("dwindow")
		server      core.Server
	)

	grpcServer, err := core.NewServerGRPC(core.ServerGRPCConfig{
		Port:        port,
		Certificate: certificate,
		Key:         key,
		DWindow:     dwindow,
	})
	must(err)
	server = &grpcServer

	err = server.Listen()
	must(err)
	defer server.Close()

	return
}
