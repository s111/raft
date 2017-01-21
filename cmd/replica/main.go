package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"os"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/relab/raft/pkg/raft"
	gorums "github.com/relab/raft/pkg/raft/gorumspb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
)

func main() {
	var (
		id               = flag.Uint64("id", 0, "server ID")
		cluster          = flag.String("cluster", ":9201", "comma separated cluster servers")
		bench            = flag.Bool("quiet", false, "Silence log output")
		recover          = flag.Bool("recover", false, "Recover from stable storage")
		cpuprofile       = flag.String("cpuprofile", "", "Write cpu profile to file")
		batch            = flag.Bool("batch", true, "enable batching")
		electionTimeout  = flag.Duration("election", 2*time.Second, "How long servers wait before starting an election")
		heartbeatTimeout = flag.Duration("heartbeat", 250*time.Millisecond, "How often a heartbeat should be sent")
		maxAppendEntries = flag.Int("maxappend", 5000, "Max entries per AppendEntries message")
	)

	flag.Parse()
	rand.Seed(time.Now().UnixNano())

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
	}

	if *id == 0 {
		fmt.Print("-id argument is required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	nodes := strings.Split(*cluster, ",")

	if len(nodes) == 0 {
		fmt.Print("-cluster argument is required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	if *maxAppendEntries < 1 {
		fmt.Print("-maxappend must be atleast 1\n\n")
		flag.Usage()
		os.Exit(1)
	}

	if *bench {
		log.SetOutput(ioutil.Discard)
		silentLogger := log.New(ioutil.Discard, "", log.LstdFlags)
		grpclog.SetLogger(silentLogger)
		grpc.EnableTracing = false
	}

	r, err := raft.NewReplica(&raft.Config{
		ID:               *id,
		Nodes:            nodes,
		Recover:          *recover,
		Batch:            *batch,
		ElectionTimeout:  *electionTimeout,
		HeartbeatTimeout: *heartbeatTimeout,
		MaxAppendEntries: *maxAppendEntries,
		Logger:           log.New(os.Stderr, "raft", log.LstdFlags),
	})

	if err != nil {
		log.Fatal(err)
	}

	s := grpc.NewServer()
	gorums.RegisterRaftServer(s, r)

	l, err := net.Listen("tcp", nodes[*id-1])

	if err != nil {
		log.Fatal(err)
	}

	go func() {
		err := s.Serve(l)

		if err != nil {
			log.Fatal(err)
		}
	}()

	if *cpuprofile != "" {
		go func() {
			if err := r.Run(); err != nil {
				log.Fatal(err)
			}
		}()

		reader := bufio.NewReader(os.Stdin)
		reader.ReadLine()

		pprof.StopCPUProfile()
	} else {
		if err := r.Run(); err != nil {
			log.Fatal(err)
		}
	}
}
