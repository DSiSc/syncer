package syncer

import (
	"github.com/DSiSc/blockchain"
	"github.com/DSiSc/craft/log"
	"github.com/DSiSc/craft/types"
	"github.com/DSiSc/p2p"
	"github.com/DSiSc/p2p/message"
	"github.com/DSiSc/syncer/common"
	"sync"
	"time"
)

// module internal sync channel.
var blockSyncChan = make(chan interface{})

// GatherNewBlockFunc gather new block from p2p network.
func GatherNewBlockFunc(block interface{}) {
	blockSyncChan <- block
}

// BlockSyncer block synchronize program
type BlockSyncer struct {
	p2p           p2p.P2PAPI
	blockChain    *blockchain.BlockChain
	eventCenter   types.EventCenter
	sendChan      chan<- interface{}
	stallChan     chan types.Hash
	quitChan      chan interface{}
	subscribers   map[types.EventType]types.Subscriber
	lock          sync.RWMutex
	pendingBlocks map[types.Hash]interface{}
}

// NewBlockSyncer create block syncer instance.
func NewBlockSyncer(p2p p2p.P2PAPI, sendChan chan<- interface{}, eventCenter types.EventCenter) (*BlockSyncer, error) {
	blockChain, err := blockchain.NewLatestStateBlockChain()
	if err != nil {
		return nil, err
	}
	return &BlockSyncer{
		p2p:         p2p,
		blockChain:  blockChain,
		sendChan:    sendChan,
		stallChan:   make(chan types.Hash),
		eventCenter: eventCenter,
		subscribers: make(map[types.EventType]types.Subscriber),
		quitChan:    make(chan interface{}),
	}, nil
}

// Start star block syncer
func (syncer *BlockSyncer) Start() error {
	go syncer.reqHandler()
	go syncer.recvHandler()

	syncer.subscribers[types.EventBlockCommitFailed] = syncer.eventCenter.Subscribe(types.EventBlockCommitFailed, GatherNewBlockFunc)
	syncer.subscribers[types.EventBlockVerifyFailed] = syncer.eventCenter.Subscribe(types.EventBlockVerifyFailed, GatherNewBlockFunc)
	syncer.subscribers[types.EventBlockCommitFailed] = syncer.eventCenter.Subscribe(types.EventBlockCommitted, GatherNewBlockFunc)
	return nil
}

// Stop stop block syncer
func (syncer *BlockSyncer) Stop() {
	syncer.lock.RLock()
	defer syncer.lock.RUnlock()
	close(syncer.quitChan)
	for eventType, subscriber := range syncer.subscribers {
		delete(syncer.subscribers, eventType)
		syncer.eventCenter.UnSubscribe(eventType, subscriber)
	}
}

// send block sync request to gather the newest block from p2p
func (syncer *BlockSyncer) reqHandler() {
	timer := time.NewTicker(10 * time.Second)
	for {
		currentBlock := syncer.blockChain.GetCurrentBlock()
		syncer.p2p.Gather(func(peerState uint64) bool {
			//TODO choose all peer as the candidate, so we can gather block more efficiently
			return true
		}, &message.BlockHeaderReq{
			Len:      1,
			HashStop: currentBlock.HeaderHash,
		})
		select {
		case <-blockSyncChan:
			continue
		case <-timer.C:
			continue
		case <-syncer.quitChan:
			return
		}
	}
}

// handle the block relative message from p2p network.
func (syncer *BlockSyncer) recvHandler() {
	msgChan := syncer.p2p.MessageChan()
	for {
		select {
		case msg := <-msgChan:
			switch msg.Payload.(type) {
			case *message.Block:
				bmsg := msg.Payload.(*message.Block)
				syncer.sendChan <- bmsg.Block
			case *message.BlockReq:
				brmsg := msg.Payload.(*message.BlockReq)
				block, err := syncer.blockChain.GetBlockByHash(brmsg.HeaderHash)
				if err != nil {
					return
				}
				bmsg := &message.Block{
					Block: block,
				}
				err = syncer.p2p.SendMsg(msg.From, bmsg)
				if err != nil {
					log.Error("failed to send message to peer %s, as: %v", msg.From.ToString(), err)
				}
			case *message.BlockHeaders:
				currentBlock := syncer.blockChain.GetCurrentBlock()
				bhmsg := msg.Payload.(*message.BlockHeaders)
				for i := 0; i < len(bhmsg.Headers); i++ {
					if bhmsg.Headers[i].Height <= currentBlock.Header.Height {
						continue
					}
					brmsg := &message.BlockReq{
						HeaderHash: common.HeaderHash(bhmsg.Headers[i]),
					}
					err := syncer.p2p.SendMsg(msg.From, brmsg)
					if err != nil {
						log.Error("failed to send message to peer %s, as: %v", msg.From.ToString(), err)
						return
					}
				}
			case *message.BlockHeaderReq:
				brmsg := msg.Payload.(*message.BlockHeaderReq)
				if brmsg.Len <= 0 {
					brmsg.Len = message.MAX_BLOCK_HEADER_NUM
				}
				blockStop, err := syncer.blockChain.GetBlockByHash(brmsg.HashStop)
				if err != nil {
					log.Error("have no block with hash %x in local database", brmsg.HashStart)
					return
				}
				blockHeaders := make([]*types.Header, 0)
				for i := 1; i <= int(brmsg.Len); i++ {
					block, err := syncer.blockChain.GetBlockByHeight(blockStop.Header.Height + uint64(i))
					if err != nil {
						log.Error("failed to get block with height %d, as:%v", blockStop.Header.Height+uint64(i), err)
						break
					}
					blockHeaders = append(blockHeaders, block.Header)
				}
				bMsg := &message.BlockHeaders{
					Headers: blockHeaders,
				}
				err = syncer.p2p.SendMsg(msg.From, bMsg)
				if err != nil {
					log.Error("failed to send message to peer %s, as: %v", msg.From.ToString(), err)
				}
			}
		case <-syncer.quitChan:
			return
		}
	}
}
