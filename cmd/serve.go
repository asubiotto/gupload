package cmd

import (
	"github.com/cirocosta/gupload/core"
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
	},
}

func serveAction(c *cli.Context) (err error) {
	var (
		port        = c.Int("port")
		http2       = c.Bool("http2")
		key         = c.String("key")
		certificate = c.String("certificate")
		server      core.Server
	)

	grpcServer, err := core.NewServerGRPC(core.ServerGRPCConfig{
		Port:        port,
		Certificate: certificate,
		Key:         key,
	})
	must(err)
	server = &grpcServer

	err = server.Listen()
	must(err)
	defer server.Close()

	return
}
