package core

import (
	"io"
	"os"
	"time"

	"github.com/asubiotto/gupload/messaging"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/keepalive"
)

// ClientGRPC provides the implementation of a file
// uploader that streams chunks via protobuf-encoded
// messages.
type ClientGRPC struct {
	logger    zerolog.Logger
	conn      *grpc.ClientConn
	client    messaging.GuploadServiceClient
	chunkSize int
}

type ClientGRPCConfig struct {
	Address         string
	ChunkSize       int
	RootCertificate string
	Compress        bool
	DWindow         bool
}

const (
	defaultWindowSize     = 65535
	initialWindowSize     = defaultWindowSize * 32 // for an RPC
	initialConnWindowSize = initialWindowSize * 16 // for a connection
)

func NewClientGRPC(cfg ClientGRPCConfig) (c ClientGRPC, err error) {
	if cfg.Address == "" {
		err = errors.Errorf("address must be specified")
		return
	}

	// Cockroach defaults.
	grpcOpts := []grpc.DialOption{
		grpc.WithBackoffMaxDelay(time.Second),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                3 * time.Second,
			Timeout:             3 * time.Second,
			PermitWithoutStream: true,
		}),
	}

	if cfg.Compress {
		grpcOpts = append(grpcOpts,
			grpc.WithDefaultCallOptions(grpc.UseCompressor("gzip")))
	}

	c.logger = zerolog.New(os.Stdout).
		With().
		Str("from", "client").
		Logger()

	if !cfg.DWindow {
		c.logger.Info().Msg("disabling dynamic window on client, setting to large cockroach defaults")
		// Set to cockroach defaults.
		grpcOpts = append(grpcOpts,
			grpc.WithInitialWindowSize(initialWindowSize),
			grpc.WithInitialConnWindowSize(initialConnWindowSize),
		)
	}

	grpcOpts = append(grpcOpts, grpc.WithInsecure())

	switch {
	case cfg.ChunkSize == 0:
		err = errors.Errorf("ChunkSize must be specified")
		return
	default:
		c.chunkSize = cfg.ChunkSize
	}

	c.conn, err = grpc.Dial(cfg.Address, grpcOpts...)
	if err != nil {
		err = errors.Wrapf(err,
			"failed to start grpc connection with address %s",
			cfg.Address)
		return
	}

	c.client = messaging.NewGuploadServiceClient(c.conn)

	return
}

func (c *ClientGRPC) UploadFile(ctx context.Context, f string) (stats Stats, err error) {
	var (
		writing = true
		buf     []byte
		n       int
		file    *os.File
		status  *messaging.UploadStatus
	)

	file, err = os.Open(f)
	if err != nil {
		err = errors.Wrapf(err,
			"failed to open file %s",
			f)
		return
	}
	defer file.Close()

	stream, err := c.client.Upload(ctx)
	if err != nil {
		err = errors.Wrapf(err,
			"failed to create upload stream for file %s",
			f)
		return
	}
	defer stream.CloseSend()

	stats.StartedAt = time.Now()
	buf = make([]byte, c.chunkSize)
	for writing {
		n, err = file.Read(buf)
		if err != nil {
			if err == io.EOF {
				writing = false
				err = nil
				continue
			}

			err = errors.Wrapf(err,
				"errored while copying from file to buf")
			return
		}

		err = stream.Send(&messaging.Chunk{
			Content: buf[:n],
		})
		if err != nil {
			err = errors.Wrapf(err,
				"failed to send chunk via stream")
			return
		}
	}

	stats.FinishedAt = time.Now()

	status, err = stream.CloseAndRecv()
	if err != nil {
		err = errors.Wrapf(err,
			"failed to receive upstream status response")
		return
	}

	if status.Code != messaging.UploadStatusCode_Ok {
		err = errors.Errorf(
			"upload failed - msg: %s",
			status.Message)
		return
	}

	return
}

func (c *ClientGRPC) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}
