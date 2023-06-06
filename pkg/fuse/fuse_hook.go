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

import "github.com/hanwen/go-fuse/v2/fuse"

type fileSystemProxy struct {
	fuse.RawFileSystem
	hook *fuseHook
}

func withHook(fs *fileSystem, hook *fuseHook) *fileSystemProxy {
	return &fileSystemProxy{
		RawFileSystem: fs,
		hook:          hook,
	}
}

func (fs *fileSystemProxy) canDo(status fuse.Status, fn func()) {
	if status == fuse.OK && fs.hook != nil {
		fn()
	}
}

func (fs *fileSystemProxy) SetAttr(cancel <-chan struct{}, input *fuse.SetAttrIn, out *fuse.AttrOut) (code fuse.Status) {
	status := fs.RawFileSystem.SetAttr(cancel, input, out)

	fs.canDo(status, func() {
		fs.hook.sendEvent(Ino(input.NodeId), Chmod)
	})
	return status
}

func (fs *fileSystemProxy) Mknod(cancel <-chan struct{}, input *fuse.MknodIn, name string, out *fuse.EntryOut) (code fuse.Status) {
	status := fs.RawFileSystem.Mknod(cancel, input, name, out)

	fs.canDo(status, func() {
		fs.hook.sendEvent(Ino(out.NodeId), Create)
	})
	return status
}

func (fs *fileSystemProxy) Mkdir(cancel <-chan struct{}, input *fuse.MkdirIn, name string, out *fuse.EntryOut) (code fuse.Status) {
	status := fs.RawFileSystem.Mkdir(cancel, input, name, out)

	fs.canDo(status, func() {
		fs.hook.sendEvent(Ino(out.NodeId), Create)
	})
	return status
}

func (fs *fileSystemProxy) Unlink(cancel <-chan struct{}, header *fuse.InHeader, name string) (code fuse.Status) {
	status := fs.RawFileSystem.Unlink(cancel, header, name)

	fs.canDo(status, func() {
		fs.hook.sendEventWithSubname(Ino(header.NodeId), name, Remove)
	})
	return status
}

func (fs *fileSystemProxy) Rmdir(cancel <-chan struct{}, header *fuse.InHeader, name string) (code fuse.Status) {
	status := fs.RawFileSystem.Rmdir(cancel, header, name)

	fs.canDo(status, func() {
		fs.hook.sendEventWithSubname(Ino(header.NodeId), name, Remove)
	})
	return status
}

func (fs *fileSystemProxy) Symlink(cancel <-chan struct{}, header *fuse.InHeader, pointedTo string, linkName string, out *fuse.EntryOut) (code fuse.Status) {
	status := fs.RawFileSystem.Symlink(cancel, header, pointedTo, linkName, out)

	fs.canDo(status, func() {
		fs.hook.sendEvent(Ino(out.NodeId), Create)
	})
	return status
}

func (fs *fileSystemProxy) Rename(cancel <-chan struct{}, input *fuse.RenameIn, oldName string, newName string) (code fuse.Status) {

	status := fs.RawFileSystem.Rename(cancel, input, oldName, newName)

	fs.canDo(status, func() {
		fs.hook.sendEventWithSubname(Ino(input.NodeId), oldName, Rename) // send event renaame of old file
		fs.hook.sendEventWithSubname(Ino(input.Newdir), newName, Create) // send create of new name
	})
	return status
}

func (fs *fileSystemProxy) Link(cancel <-chan struct{}, input *fuse.LinkIn, name string, out *fuse.EntryOut) (code fuse.Status) {
	status := fs.RawFileSystem.Link(cancel, input, name, out)

	fs.canDo(status, func() {
		fs.hook.sendEvent(Ino(out.NodeId), Create)
	})
	return status
}

func (fs *fileSystemProxy) SetXAttr(cancel <-chan struct{}, input *fuse.SetXAttrIn, attr string, data []byte) fuse.Status {
	status := fs.RawFileSystem.SetXAttr(cancel, input, attr, data)

	fs.canDo(status, func() {
		fs.hook.sendEvent(Ino(input.NodeId), Chmod)
	})
	return status
}

func (fs *fileSystemProxy) RemoveXAttr(cancel <-chan struct{}, header *fuse.InHeader, attr string) fuse.Status {
	status := fs.RawFileSystem.RemoveXAttr(cancel, header, attr)

	fs.canDo(status, func() {
		fs.hook.sendEvent(Ino(header.NodeId), Chmod)
	})
	return status
}

func (fs *fileSystemProxy) Create(cancel <-chan struct{}, input *fuse.CreateIn, name string, out *fuse.CreateOut) (code fuse.Status) {
	status := fs.RawFileSystem.Create(cancel, input, name, out)

	fs.canDo(status, func() {
		fs.hook.sendEvent(Ino(out.NodeId), Create)
	})
	return status
}

func (fs *fileSystemProxy) Write(cancel <-chan struct{}, input *fuse.WriteIn, data []byte) (written uint32, code fuse.Status) {
	w, status := fs.RawFileSystem.Write(cancel, input, data)

	fs.canDo(status, func() {
		fs.hook.sendEvent(Ino(input.NodeId), Write)
	})
	return w, status
}

func (fs *fileSystemProxy) Flush(cancel <-chan struct{}, input *fuse.FlushIn) fuse.Status {
	status := fs.RawFileSystem.Flush(cancel, input)

	// openning file to read will flush too
	// fs.canDo(status, func() {
	// 	fs.hook.sendEvent(Ino(input.NodeId), Write)
	// })
	return status
}

func (fs *fileSystemProxy) Fsync(cancel <-chan struct{}, input *fuse.FsyncIn) (code fuse.Status) {
	status := fs.RawFileSystem.Fsync(cancel, input)

	fs.canDo(status, func() {
		fs.hook.sendEvent(Ino(input.NodeId), Write)
	})
	return status
}

func (fs *fileSystemProxy) FsyncDir(cancel <-chan struct{}, input *fuse.FsyncIn) (code fuse.Status) {
	status := fs.RawFileSystem.FsyncDir(cancel, input)

	fs.canDo(status, func() {
		fs.hook.sendEvent(Ino(input.NodeId), Write)
	})
	return status
}

func (fs *fileSystemProxy) Fallocate(cancel <-chan struct{}, in *fuse.FallocateIn) (code fuse.Status) {
	status := fs.RawFileSystem.Fallocate(cancel, in)

	fs.canDo(status, func() {
		fs.hook.sendEvent(Ino(in.NodeId), Write)
	})
	return status
}

func (fs *fileSystemProxy) CopyFileRange(cancel <-chan struct{}, input *fuse.CopyFileRangeIn) (written uint32, code fuse.Status) {
	w, status := fs.RawFileSystem.CopyFileRange(cancel, input)

	fs.canDo(status, func() {
		fs.hook.sendEvent(Ino(input.NodeIdOut), Write)
	})
	return w, status
}

/*
func (fs *fileSystemProxy) Access(cancel <-chan struct{}, input *fuse.AccessIn) (code fuse.Status) {
	return ENOSYS
}


func (fs *fileSystemProxy) OpenDir(cancel <-chan struct{}, input *fuse.OpenIn, out *fuse.OpenOut) (status fuse.Status) {
	return ENOSYS
}

func (fs *fileSystemProxy) SetLk(cancel <-chan struct{}, in *fuse.LkIn) (code fuse.Status) {
	return ENOSYS
}

func (fs *fileSystemProxy) SetLkw(cancel <-chan struct{}, in *fuse.LkIn) (code fuse.Status) {
	return ENOSYS
}

func (fs *fileSystemProxy) Release(cancel <-chan struct{}, input *fuse.ReleaseIn) {
	fs.RawFileSystem.Release(cancel, input)
}

func (fs *fileSystemProxy) ReleaseDir(input *fuse.ReleaseIn) {
}

func (fs *fileSystemProxy) Lseek(cancel <-chan struct{}, in *fuse.LseekIn, out *fuse.LseekOut) fuse.Status {
	return ENOSYS
}

func (fs *fileSystemProxy) Ioctl(cancel <-chan struct{}, in *fuse.IoctlIn, out *fuse.IoctlOut, bufIn, bufOut []byte) fuse.Status {
	return ENOSYS
}
*/
