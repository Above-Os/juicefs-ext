// Copyright 2023 bytetrade
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package fuse

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"strings"
	"time"
	"unsafe"

	ipc "github.com/james-barrow/golang-ipc"
	"github.com/juicedata/juicefs/pkg/meta"
)

// copy from fsnotify (https://github.com/fsnotify/fsnotify/blob/main/fsnotify.go)

// Event represents a file system notification.
type Event struct {
	// Path to the file or directory.
	//
	// Paths are relative to the input; for example with Add("dir") the Name
	// will be set to "dir/file" if you create that file, but if you use
	// Add("/path/to/dir") it will be "/path/to/dir/file".
	Name string

	// File operation that triggered the event.
	//
	// This is a bitmask and some systems may send multiple operations at once.
	// Use the Event.Has() method instead of comparing with ==.
	Op Op

	WatchKey string
}

const EventPackSize = meta.MaxName + 4 + meta.MaxName

// Op describes a set of file operations.
type Op uint32

// The operations fsnotify can trigger; see the documentation on [Watcher] for a
// full description, and check them with [Event.Has].
const (
	// A new pathname was created.
	Create Op = 1 << iota

	// The pathname was written to; this does *not* mean the write has finished,
	// and a write can be followed by more writes.
	Write

	// The path was removed; any watches on it will be removed. Some "remove"
	// operations may trigger a Rename if the file is actually moved (for
	// example "remove to trash" is often a rename).
	Remove

	// The path was renamed to something else; any watched on it will be
	// removed.
	Rename

	// File attributes were changed.
	//
	// It's generally not recommended to take action on this event, as it may
	// get triggered very frequently by some software. For example, Spotlight
	// indexing on macOS, anti-virus software, backup software, etc.
	Chmod
)

const (
	MSG_ERROR = iota

	// message data:
	// {
	// 	byte[255] // watcher name
	// 	byte[255][] // path array
	// }
	MSG_WATCH

	// message data:
	// {
	// 	byte[255] // watcher name
	// 	byte[255][] // path array
	// }
	MSG_UNWATCH

	// message data:
	// {
	// 	byte[255] // watcher name
	// }
	MSG_CLEAR

	// message data:
	// {
	// 	byte[255] // watcher name
	// }
	MSG_SUSPEND

	// message data:
	// {
	// 	byte[255] // watcher name
	// }
	MSG_RESUME

	// message data:
	// {
	// 	byte[255] // path
	//  int32 // op
	//  byte[255] // key
	// }[] // event array
	MSG_EVENT
)

func (o Op) String() string {
	var s []string
	if o&Create == Create {
		s = append(s, "Create")
	}

	if o&Write == Write {
		s = append(s, "Write")
	}

	if o&Remove == Remove {
		s = append(s, "Remove")
	}

	if o&Chmod == Chmod {
		s = append(s, "Chmod")
	}

	return strings.Join(s, "|")
}

// type Watcher struct {
// 	Path []byte
// }

func min(x, y int) int {
	if x > y {
		return y
	}

	return x
}

func withNotify(hook *fuseHook, close <-chan struct{}) *fuseHook {

	getPaths := func(msg *ipc.Message) []string {
		var paths []string
		offset := 0
		n := len(msg.Data)
		for offset < n {
			strlen := min(n-offset, meta.MaxName)
			paths = append(paths, string(bytes.Trim(msg.Data[offset:offset+strlen], "\x00")))
			offset += meta.MaxName
		}

		// FIXME: support multi-client
		if len(paths) <= 1 {
			return []string{}
		}

		return paths[1:]
	}

	n := &notifyServer{
		handler: func(msg *ipc.Message) {
			switch msg.MsgType {
			case MSG_WATCH:
				hook.addWatch(getPaths(msg))
			case MSG_UNWATCH:
				hook.unwatch(getPaths(msg))
			case MSG_SUSPEND:
				hook.suspend()
			case MSG_RESUME:
				hook.resume()
			case MSG_CLEAR:
				hook.clearWatch()
			}
		},
		evenQ: make(chan Event, 4096),
		waitQ: make(chan struct{}, 100), // wake up write worker
		done:  make(chan int),
	}

	n.serve()

	go func() {
		<-close
		n.close()
	}()

	hook.notify = n

	return hook
}

type fsnotify interface {
	notify(e Event)
}

type notifyServer struct {
	fsnotify
	server    *ipc.Server
	handler   func(msg *ipc.Message)
	evenQ     chan Event
	waitQ     chan struct{}
	closed    bool
	done      chan int
	suspended bool
}

// watch server currently supports just one to one ipc
func (n *notifyServer) serve() {
	sc, err := ipc.StartServer("JuiceFS-IPC", &ipc.ServerConfig{Encryption: false})
	if err != nil {
		panic(err)
	}

	n.server = sc

	go func() {
		tick := time.NewTicker(500 * time.Millisecond)

		for {
			select {
			case <-tick.C:
				switch n.server.StatusCode() {
				case ipc.Connected:
					if n.handler != nil && n.suspended {
						n.handler(&ipc.Message{MsgType: MSG_RESUME})
						n.suspended = false
					}
				default:
					if n.handler != nil && !n.suspended {
						n.handler(&ipc.Message{MsgType: MSG_SUSPEND})
						n.suspended = true
					}
				}
			case <-n.done:
				return
			}

		}
	}()

	// read
	go func() {
		for {
			m, err := sc.Read()

			if err == nil {
				if m.MsgType > 0 {
					logger.Debug("Server recieved: "+string(m.Data)+":"+strconv.Itoa(len(m.Data))+" - Message type: ", m.MsgType)
					if n.handler != nil {
						n.handler(m)
					}
				}

			} else {
				logger.Error("IPC Server error, ", err)
				sc.Close()

				// recreate server for next loop
				sc, err = ipc.StartServer("JuiceFS-IPC", nil)
				if err != nil {
					panic(err)
				}

				n.server = sc

				logger.Info("IPC Server recreated")

			}
		}
	}()

	// batch write
	go func() {
		for !n.closed {
			<-n.waitQ

			var batchEvent []Event
			func() {
				for {
					select {
					case e := <-n.evenQ:
						// TODO: merge event
						batchEvent = append(batchEvent, e)
					default:
						return
					}
				}
			}()

			if len(batchEvent) > 0 {
				msg := n.packageMsgData(batchEvent)
				err := sc.Write(MSG_EVENT, msg)

				if err != nil {
					logger.Error("send event to cluster error, ", err)
				}
			}
		}
	}()
}

func (n *notifyServer) close() {
	n.server.Close()
	close(n.evenQ)
	close(n.waitQ)
	close(n.done)
	n.closed = true
}

func (n *notifyServer) notify(e Event) {
	n.evenQ <- e
	n.waitQ <- struct{}{}
}

func (n *notifyServer) packageMsgData(events []Event) []byte {
	frame := make([]byte, EventPackSize*len(events))
	offset := 0
	for _, e := range events {
		data := ([]byte)(unsafe.Slice(&frame[offset], meta.MaxName))
		copy(data, []byte(e.Name))
		offset += meta.MaxName

		data = ([]byte)(unsafe.Slice(&frame[offset], 4))
		binary.BigEndian.PutUint32(data, uint32(e.Op))
		offset += 4

		data = ([]byte)(unsafe.Slice(&frame[offset], meta.MaxName))
		copy(data, []byte(e.WatchKey))
		offset += meta.MaxName
	}

	return frame
}
