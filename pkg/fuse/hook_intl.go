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
	"strconv"
	"strings"
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

func (o *fuseHook) matchPathNames(i Ino, s string) [][]string {
	paths := meta.GetPaths(o.v.Meta, o.ctx, i)

	matchPath := func(paths []string, sub string) (p, k string) {
		if sub != "" {
			paths = append(paths, sub)
		}

		try := len(paths)

		var (
			ok bool
		)

		o.lock.RLock()
		for try > 0 {
			k = filepath.Clean(o.v.Conf.Meta.MountPoint + "/" + filepath.Join(paths[:try]...))
			logger.Debug("match path, [", k, " try ", strconv.Itoa(try))
			if _, ok = o.watchList[k]; ok {
				break
			}

			try--
		}
		o.lock.RUnlock()

		if !ok {
			return "", ""
		}

		p = filepath.Join(paths...)

		return filepath.Clean(o.v.Conf.Meta.MountPoint + "/" + p), k
	}

	var ret [][]string
	for _, p := range paths {
		ptoken := strings.Split(p, string(filepath.Separator))
		if p, k := matchPath(ptoken, s); p != "" && k != "" {
			ret = append(ret, []string{p, k})
		}
	}

	return ret
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

	ret := o.matchPathNames(i, subname)
	for _, r := range ret {
		path, watchKey := r[0], r[1]

		if watchKey != "" {
			logger.Debug("send event to pipe, ", i, " , ", path, " , ", op)
			o.notify.notify(Event{
				Name:     path,
				Op:       op,
				WatchKey: watchKey,
			})
		}
	}
}

func (o *fuseHook) addWatch(paths []string) {
	o.lock.Lock()
	defer o.lock.Unlock()

	for _, p := range paths {
		logger.Debug("add watch ", p)
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
