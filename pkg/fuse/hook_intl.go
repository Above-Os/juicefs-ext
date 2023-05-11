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
	"path/filepath"
	"sync"

	"github.com/juicedata/juicefs/pkg/meta"
	"github.com/juicedata/juicefs/pkg/vfs"
)

type fuseHook struct {
	v         *vfs.VFS
	ctx       meta.Context
	lock      sync.RWMutex
	watchList map[string]int
	notify    fsnotify
	suspended bool
}

func newFuseHook(v *vfs.VFS, ctx meta.Context) *fuseHook {
	return &fuseHook{
		v:         v,
		ctx:       ctx,
		watchList: make(map[string]int),
	}
}

func (o *fuseHook) sendEvent(i Ino, op Op) {
	o.sendEventWithSubname(i, "", op)
}

func (o *fuseHook) matchPathName(i Ino, sub string) (path, key string) {
	paths := meta.GetPaths(o.v.Meta, o.ctx, i)
	if sub != "" {
		paths = append(paths, sub)
	}

	try := len(paths)

	var (
		ok bool
	)

	o.lock.RLock()
	for ; try > 0 && !ok; _, ok = o.watchList[filepath.Join(paths[:try]...)] {
		try--
	}
	o.lock.RUnlock()

	if !ok {
		return "", ""
	}

	key = filepath.Join(paths[:try]...)
	path = filepath.Join(paths...)

	return o.v.Conf.Meta.MountPoint + path, key
}

func (o *fuseHook) sendEventWithSubname(i Ino, subname string, op Op) {
	if func() bool {
		o.lock.RLock()
		defer o.lock.RUnlock()

		if o.suspended {
			return true
		}

		if len(o.watchList) == 0 {
			return true
		}

		return false
	}() {
		return
	}

	path, watchKey := o.matchPathName(i, subname)

	if watchKey != "" {
		logger.Debug("send event to pipe, ", i, " , ", path, " , ", op)
		o.notify.notify(Event{
			Name:     path,
			Op:       op,
			WatchKey: watchKey,
		})
	}
}

func (o *fuseHook) addWatch(paths []string) {
	o.lock.Lock()
	defer o.lock.Unlock()

	for _, p := range paths {
		o.watchList[p] = 1
	}
}

func (o *fuseHook) unwatch(paths []string) {
	o.lock.Lock()
	defer o.lock.Unlock()

	for _, p := range paths {
		delete(o.watchList, p)
	}
}

func (o *fuseHook) clearWatch() {
	for k := range o.watchList {
		delete(o.watchList, k)
	}
}

func (o *fuseHook) suspend() {
	o.lock.Lock()
	defer o.lock.Unlock()

	o.suspended = true
}

func (o *fuseHook) resume() {
	o.lock.Lock()
	defer o.lock.Unlock()

	o.suspended = false
}
