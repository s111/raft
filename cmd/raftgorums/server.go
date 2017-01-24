package main

import (
	"golang.org/x/net/context"

	"github.com/relab/raft"
	pb "github.com/relab/raft/raftpb"
)

type RaftServer struct {
	*raft.Replica
}

func (r *RaftServer) RequestVote(ctx context.Context, req *pb.RequestVoteRequest) (*pb.RequestVoteResponse, error) {
	return r.HandleRequestVoteRequest(req), nil
}

func (r *RaftServer) AppendEntries(ctx context.Context, req *pb.AppendEntriesRequest) (*pb.AppendEntriesResponse, error) {
	return r.HandleAppendEntriesRequest(req), nil
}

func (r *RaftServer) ClientCommand(ctx context.Context, req *pb.ClientCommandRequest) (*pb.ClientCommandResponse, error) {
	return r.HandleClientCommandRequest(req)
}
