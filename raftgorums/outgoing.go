package raftgorums

import (
	"time"

	"golang.org/x/net/context"

	"github.com/Sirupsen/logrus"
	pb "github.com/relab/raft/raftgorums/raftpb"
)

func (r *Raft) handleOutgoing() error {
	// January 1, 1970 UTC.
	var lastCuReq time.Time

	for {
		select {
		case <-r.stop:
			return nil
		case err := <-r.mem.get().SubError():
			// TODO If a node becomes unavailable and there is a
			// backup available in the same or an alternate region,
			// instantiate reconfiguratior. TODO How many errors
			// before a node is considered unavailable? If there is
			// no backup node available, don't do anything, but
			// schedule the reconfiguratior.
			r.logger.WithField("nodeid", err.NodeID).Warnln("Node unavailable")
		case req := <-r.cureqout:
			// TODO Use config.
			if time.Since(lastCuReq) < 100*time.Millisecond {
				continue
			}
			lastCuReq = time.Now()

			r.logger.WithField("matchindex", req.matchIndex).Warnln("Sending catch-up")
			ctx, cancel := context.WithTimeout(context.Background(), TCPHeartbeat*time.Millisecond)
			leader := r.mem.getNode(req.leaderID)
			_, err := leader.RaftClient.CatchMeUp(ctx, &pb.CatchMeUpRequest{
				FollowerID: r.id,
				NextIndex:  req.matchIndex + 1,
			})
			cancel()

			if err != nil {
				r.logger.WithError(err).Warnln("CatchMeUp failed")
			}
		case req := <-r.rvreqout:
			conf := r.mem.get()

			r.logger.WithField("conf", conf.NodeIDs()).Println("Sending request for vote")

			ctx, cancel := context.WithTimeout(context.Background(), TCPHeartbeat*time.Millisecond)
			res, err := conf.RequestVote(ctx, req)
			cancel()

			if err != nil {
				r.logger.WithError(err).Warnln("RequestVote failed")
			}

			if res == nil {
				continue
			}

			r.HandleRequestVoteResponse(res)

		case req := <-r.aereqout:
			next := make(map[uint32]uint64)
			nextIndex := req.PrevLogIndex + 1

			for nodeID, ch := range r.match {
				select {
				case index := <-ch:
					atLeastMaxEntries := req.PrevLogIndex+1 > r.burst
					tooFarBehind := index < req.PrevLogIndex+1-r.burst

					if atLeastMaxEntries && tooFarBehind {
						r.logger.WithFields(logrus.Fields{
							"gorumsid": nodeID,
						}).Warnln("Server too far behind")
						index = req.PrevLogIndex + 1
					}
					next[nodeID] = index
					if index < nextIndex {
						nextIndex = index
					}
				default:
				}
			}

			// TODO This should be safe as it only accesses storage
			// which uses transactions. TODO It accesses
			// maxAppendEntries but this on does not change after
			// startup.
			entries := r.getNextEntries(nextIndex)
			e := uint64(len(entries))
			maxIndex := nextIndex + e - 1

			conf := r.mem.get()
			r.logger.WithField("conf", conf.NodeIDs()).Println("Sending append entries request")

			ctx, cancel := context.WithTimeout(context.Background(), TCPHeartbeat*time.Millisecond)
			res, err := conf.AppendEntries(ctx, req,
				// These functions will be executed concurrently.
				func(req pb.AppendEntriesRequest, nodeID uint32) *pb.AppendEntriesRequest {
					if index, ok := next[nodeID]; ok {
						req.PrevLogIndex = index - 1
						// TODO This should be safe as
						// it only accesses storage
						// which uses transactions.
						req.PrevLogTerm = r.logTerm(index - 1)
					}

					need := maxIndex - req.PrevLogIndex
					req.Entries = entries[e-need:]

					r.logger.WithFields(logrus.Fields{
						"prevlogindex": req.PrevLogIndex,
						"prevlogterm":  req.PrevLogTerm,
						"commitindex":  req.CommitIndex,
						"currentterm":  req.Term,
						"lenentries":   len(req.Entries),
						"gorumsid":     nodeID,
					}).Infoln("Sending AppendEntries")

					return &req
				},
			)

			if err != nil {
				r.logger.WithError(err).Warnln("AppendEntries failed")
			}

			if res == nil {
				// This will cause the leader to step down as
				// len(res.Replies) == 0.
				res = &pb.AppendEntriesQFResponse{
					Term: req.Term,
				}
			}

			// Cancel on abort.
			if !res.Success {
				cancel()
			}

			r.HandleAppendEntriesResponse(req, res, res.Replies)
		}
	}
}
