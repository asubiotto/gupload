package core

import (
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/keepalive"

	"github.com/asubiotto/gupload/messaging"
	"github.com/dustin/go-humanize"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	_ "google.golang.org/grpc/encoding/gzip"
)

type ServerGRPC struct {
	logger      zerolog.Logger
	server      *grpc.Server
	port        int
	certificate string
	key         string
	dwindow     bool
}

type ServerGRPCConfig struct {
	Certificate string
	Key         string
	Port        int
	DWindow     bool
}

func NewServerGRPC(cfg ServerGRPCConfig) (s ServerGRPC, err error) {
	s.logger = zerolog.New(os.Stdout).
		With().
		Str("from", "server").
		Logger()

	if cfg.Port == 0 {
		err = errors.Errorf("Port must be specified")
		return
	}

	s.port = cfg.Port
	s.certificate = cfg.Certificate
	s.key = cfg.Key
	s.dwindow = cfg.DWindow

	return
}

func (s *ServerGRPC) Listen() (err error) {
	ka := keepalive.ServerParameters{
		Time:    3 * time.Second,
		Timeout: 3 * time.Second,
	}
	ke := keepalive.EnforcementPolicy{
		MinTime:             time.Nanosecond,
		PermitWithoutStream: true,
	}
	var listener net.Listener
	grpcOpts := []grpc.ServerOption{
		// The limiting factor for lowering the max message size is the fact
		// that a single large kv can be sent over the network in one message.
		// Our maximum kv size is unlimited, so we need this to be very large.
		//
		// TODO(peter,tamird): need tests before lowering.
		grpc.MaxRecvMsgSize(math.MaxInt32),
		grpc.MaxSendMsgSize(math.MaxInt32),
		// The default number of concurrent streams/requests on a client connection
		// is 100, while the server is unlimited. The client setting can only be
		// controlled by adjusting the server value. Set a very large value for the
		// server value so that we have no fixed limit on the number of concurrent
		// streams/requests on either the client or server.
		grpc.MaxConcurrentStreams(math.MaxInt32),
		grpc.KeepaliveParams(ka),
		grpc.KeepaliveEnforcementPolicy(ke),
		// A stats handler to measure server network stats.
		// grpc.StatsHandler(&ctx.stats),
	}

	if !s.dwindow {
		// Adjust the stream and connection window sizes to cockroach defaults,
		// This disables dynamic window resizing.
		grpcOpts = append(
			grpcOpts,
			grpc.InitialWindowSize(initialWindowSize),
			grpc.InitialConnWindowSize(initialConnWindowSize),
		)
	}

	listener, err = net.Listen("tcp", ":"+strconv.Itoa(s.port))
	if err != nil {
		err = errors.Wrapf(err,
			"failed to listen on port %d",
			s.port)
		return
	}

	s.server = grpc.NewServer(grpcOpts...)
	messaging.RegisterGuploadServiceServer(s.server, s)

	err = s.server.Serve(listener)
	if err != nil {
		err = errors.Wrapf(err, "errored listening for grpc connections")
		return
	}

	return
}

func (s *ServerGRPC) Upload(stream messaging.GuploadService_UploadServer) error {
	f, err := os.Create("data.json")
	if err != nil {
		s.logger.Error().Msg(fmt.Sprintf("errors opening data file: %s", err.Error()))
		return err
	}
	defer f.Close()

	var bytesReceived uint64

	s.logger.Info().Msg("received upload")
	defer s.logger.Info().Msg("upload finished")

	done := make(chan struct{})
	defer close(done)
	go func() {
		start := time.Now()
		tick := 0
		var lastBytesReceived uint64
		for {
			select {
			case <-time.After(time.Second):
			case <-done:
				return
			}
			bytesSinceStart := atomic.LoadUint64(&bytesReceived)
			bytesInLastSecond := bytesSinceStart - lastBytesReceived
			bytesPerSecond := bytesSinceStart / uint64(time.Since(start).Seconds())
			// As CSV.
			data := fmt.Sprintf("%d,%d,%d\n", tick, bytesPerSecond, bytesInLastSecond)
			msg := fmt.Sprintf("tick: %d, avg: %s/s, bytes since last tick: %d", tick, humanize.Bytes(bytesPerSecond), bytesInLastSecond)
			s.logger.Info().Msg(msg)
			f.WriteString(data)
			tick++
			lastBytesReceived = bytesSinceStart
		}
	}()

	for {
		c, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				goto END
			}

			return errors.Wrapf(err,
				"failed unexpectadely while reading chunks from stream")
		}
		atomic.AddUint64(&bytesReceived, uint64(len(c.Content)))
	}

	s.logger.Info().Msg("upload received")

END:
	err = stream.SendAndClose(&messaging.UploadStatus{
		Message: "Upload received with success",
		Code:    messaging.UploadStatusCode_Ok,
	})
	if err != nil {
		return errors.Wrapf(err,
			"failed to send status code")
	}

	return nil
}

func (s *ServerGRPC) Close() {
	if s.server != nil {
		s.server.Stop()
	}

	return
}
