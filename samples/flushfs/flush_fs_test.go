// Copyright 2015 Google Inc. All Rights Reserved.
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

package flushfs_test

import (
	"io"
	"io/ioutil"
	"os"
	"path"
	"runtime"
	"sync"
	"syscall"
	"testing"

	"github.com/jacobsa/fuse"
	"github.com/jacobsa/fuse/samples"
	"github.com/jacobsa/fuse/samples/flushfs"
	. "github.com/jacobsa/oglematchers"
	. "github.com/jacobsa/ogletest"
)

func TestFlushFS(t *testing.T) { RunTests(t) }

////////////////////////////////////////////////////////////////////////
// Boilerplate
////////////////////////////////////////////////////////////////////////

type FlushFSTest struct {
	samples.SampleTest

	// File handles that are closed in TearDown if non-nil.
	f1 *os.File
	f2 *os.File

	mu sync.Mutex

	// GUARDED_BY(mu)
	flushes  []string
	flushErr error

	// GUARDED_BY(mu)
	fsyncs   []string
	fsyncErr error
}

func init() { RegisterTestSuite(&FlushFSTest{}) }

func (t *FlushFSTest) SetUp(ti *TestInfo) {
	var err error

	// Set up a file system.
	reportTo := func(slice *[]string, err *error) func(string) error {
		return func(s string) error {
			t.mu.Lock()
			defer t.mu.Unlock()

			*slice = append(*slice, s)
			return *err
		}
	}

	t.FileSystem, err = flushfs.NewFileSystem(
		reportTo(&t.flushes, &t.flushErr),
		reportTo(&t.fsyncs, &t.fsyncErr))

	if err != nil {
		panic(err)
	}

	// Mount it.
	t.SampleTest.SetUp(ti)
}

func (t *FlushFSTest) TearDown() {
	// Close files if non-nil.
	if t.f1 != nil {
		ExpectEq(nil, t.f1.Close())
	}

	if t.f2 != nil {
		ExpectEq(nil, t.f2.Close())
	}

	// Finish tearing down.
	t.SampleTest.TearDown()
}

////////////////////////////////////////////////////////////////////////
// Helpers
////////////////////////////////////////////////////////////////////////

// Return a copy of the current contents of t.flushes.
//
// LOCKS_EXCLUDED(t.mu)
func (t *FlushFSTest) getFlushes() (p []string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	p = make([]string, len(t.flushes))
	copy(p, t.flushes)
	return
}

// Return a copy of the current contents of t.fsyncs.
//
// LOCKS_EXCLUDED(t.mu)
func (t *FlushFSTest) getFsyncs() (p []string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	p = make([]string, len(t.fsyncs))
	copy(p, t.fsyncs)
	return
}

// LOCKS_EXCLUDED(t.mu)
func (t *FlushFSTest) setFlushError(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.flushErr = err
}

// LOCKS_EXCLUDED(t.mu)
func (t *FlushFSTest) setFsyncError(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.fsyncErr = err
}

// Like syscall.Dup2, but correctly annotates the syscall as blocking. See here
// for more info: https://github.com/golang/go/issues/10202
func dup2(oldfd int, newfd int) (err error) {
	_, _, e1 := syscall.Syscall(
		syscall.SYS_DUP2, uintptr(oldfd), uintptr(newfd), 0)

	if e1 != 0 {
		err = e1
	}

	return
}

////////////////////////////////////////////////////////////////////////
// Tests
////////////////////////////////////////////////////////////////////////

func (t *FlushFSTest) CloseReports_ReadWrite() {
	var n int
	var off int64
	var err error
	buf := make([]byte, 1024)

	// Open the file.
	t.f1, err = os.OpenFile(path.Join(t.Dir, "foo"), os.O_RDWR, 0)
	AssertEq(nil, err)

	// Write some contents to the file.
	n, err = t.f1.Write([]byte("taco"))
	AssertEq(nil, err)
	AssertEq(4, n)

	// Seek and read them back.
	off, err = t.f1.Seek(0, 0)
	AssertEq(nil, err)
	AssertEq(0, off)

	n, err = t.f1.Read(buf)
	AssertThat(err, AnyOf(nil, io.EOF))
	AssertEq("taco", string(buf[:n]))

	// At this point, no flushes or fsyncs should have happened.
	AssertThat(t.getFlushes(), ElementsAre())
	AssertThat(t.getFsyncs(), ElementsAre())

	// Close the file.
	err = t.f1.Close()
	t.f1 = nil
	AssertEq(nil, err)

	// Now we should have received the flush operation (but still no fsync).
	ExpectThat(t.getFlushes(), ElementsAre("taco"))
	ExpectThat(t.getFsyncs(), ElementsAre())
}

func (t *FlushFSTest) CloseReports_ReadOnly() {
	var err error

	// Open the file.
	t.f1, err = os.OpenFile(path.Join(t.Dir, "foo"), os.O_RDONLY, 0)
	AssertEq(nil, err)

	// At this point, no flushes or fsyncs should have happened.
	AssertThat(t.getFlushes(), ElementsAre())
	AssertThat(t.getFsyncs(), ElementsAre())

	// Close the file.
	err = t.f1.Close()
	t.f1 = nil
	AssertEq(nil, err)

	// Now we should have received the flush operation (but still no fsync).
	ExpectThat(t.getFlushes(), ElementsAre(""))
	ExpectThat(t.getFsyncs(), ElementsAre())
}

func (t *FlushFSTest) CloseReports_WriteOnly() {
	var n int
	var err error

	// Open the file.
	t.f1, err = os.OpenFile(path.Join(t.Dir, "foo"), os.O_WRONLY, 0)
	AssertEq(nil, err)

	// Write some contents to the file.
	n, err = t.f1.Write([]byte("taco"))
	AssertEq(nil, err)
	AssertEq(4, n)

	// At this point, no flushes or fsyncs should have happened.
	AssertThat(t.getFlushes(), ElementsAre())
	AssertThat(t.getFsyncs(), ElementsAre())

	// Close the file.
	err = t.f1.Close()
	t.f1 = nil
	AssertEq(nil, err)

	// Now we should have received the flush operation (but still no fsync).
	ExpectThat(t.getFlushes(), ElementsAre("taco"))
	ExpectThat(t.getFsyncs(), ElementsAre())
}

func (t *FlushFSTest) CloseReports_MultipleTimes_NonOverlappingFileHandles() {
	var n int
	var err error

	// Open the file.
	t.f1, err = os.OpenFile(path.Join(t.Dir, "foo"), os.O_WRONLY, 0)
	AssertEq(nil, err)

	// Write some contents to the file.
	n, err = t.f1.Write([]byte("taco"))
	AssertEq(nil, err)
	AssertEq(4, n)

	// At this point, no flushes or fsyncs should have happened.
	AssertThat(t.getFlushes(), ElementsAre())
	AssertThat(t.getFsyncs(), ElementsAre())

	// Close the file.
	err = t.f1.Close()
	t.f1 = nil
	AssertEq(nil, err)

	// Now we should have received the flush operation (but still no fsync).
	AssertThat(t.getFlushes(), ElementsAre("taco"))
	AssertThat(t.getFsyncs(), ElementsAre())

	// Open the file again.
	t.f1, err = os.OpenFile(path.Join(t.Dir, "foo"), os.O_WRONLY, 0)
	AssertEq(nil, err)

	// Write again; expect no further flushes.
	n, err = t.f1.Write([]byte("p"))
	AssertEq(nil, err)
	AssertEq(1, n)

	AssertThat(t.getFlushes(), ElementsAre("taco"))
	AssertThat(t.getFsyncs(), ElementsAre())

	// Close the file. Now the new contents should be flushed.
	err = t.f1.Close()
	t.f1 = nil
	AssertEq(nil, err)

	AssertThat(t.getFlushes(), ElementsAre("taco", "paco"))
	AssertThat(t.getFsyncs(), ElementsAre())
}

func (t *FlushFSTest) CloseReports_MultipleTimes_OverlappingFileHandles() {
	var n int
	var err error

	// Open the file with two handles.
	t.f1, err = os.OpenFile(path.Join(t.Dir, "foo"), os.O_WRONLY, 0)
	AssertEq(nil, err)

	t.f2, err = os.OpenFile(path.Join(t.Dir, "foo"), os.O_WRONLY, 0)
	AssertEq(nil, err)

	// Write some contents with each handle.
	n, err = t.f1.Write([]byte("taco"))
	AssertEq(nil, err)
	AssertEq(4, n)

	n, err = t.f2.Write([]byte("p"))
	AssertEq(nil, err)
	AssertEq(1, n)

	// At this point, no flushes or fsyncs should have happened.
	AssertThat(t.getFlushes(), ElementsAre())
	AssertThat(t.getFsyncs(), ElementsAre())

	// Close one handle. The current contents should be flushed.
	err = t.f1.Close()
	t.f1 = nil
	AssertEq(nil, err)

	AssertThat(t.getFlushes(), ElementsAre("paco"))
	AssertThat(t.getFsyncs(), ElementsAre())

	// Write some more contents via the other handle. Again, no further flushes.
	n, err = t.f2.Write([]byte("orp"))
	AssertEq(nil, err)
	AssertEq(3, n)

	AssertThat(t.getFlushes(), ElementsAre("paco"))
	AssertThat(t.getFsyncs(), ElementsAre())

	// Close the handle. Now the new contents should be flushed.
	err = t.f2.Close()
	t.f2 = nil
	AssertEq(nil, err)

	AssertThat(t.getFlushes(), ElementsAre("paco", "porp"))
	AssertThat(t.getFsyncs(), ElementsAre())
}

func (t *FlushFSTest) CloseError() {
	var err error

	// Open the file.
	t.f1, err = os.OpenFile(path.Join(t.Dir, "foo"), os.O_RDWR, 0)
	AssertEq(nil, err)

	// Configure a flush error.
	t.setFlushError(fuse.ENOENT)

	// Close the file.
	err = t.f1.Close()
	t.f1 = nil

	AssertNe(nil, err)
	ExpectThat(err, Error(HasSubstr("no such file")))
}

func (t *FlushFSTest) FsyncReports() {
	var n int
	var err error

	// Open the file.
	t.f1, err = os.OpenFile(path.Join(t.Dir, "foo"), os.O_WRONLY, 0)
	AssertEq(nil, err)

	// Write some contents to the file.
	n, err = t.f1.Write([]byte("taco"))
	AssertEq(nil, err)
	AssertEq(4, n)

	AssertThat(t.getFlushes(), ElementsAre())
	AssertThat(t.getFsyncs(), ElementsAre())

	// Fsync.
	err = t.f1.Sync()
	AssertEq(nil, err)

	AssertThat(t.getFlushes(), ElementsAre())
	AssertThat(t.getFsyncs(), ElementsAre("taco"))

	// Write some more contents.
	n, err = t.f1.Write([]byte("s"))
	AssertEq(nil, err)
	AssertEq(1, n)

	AssertThat(t.getFlushes(), ElementsAre())
	AssertThat(t.getFsyncs(), ElementsAre("taco"))

	// Fsync.
	err = t.f1.Sync()
	AssertEq(nil, err)

	AssertThat(t.getFlushes(), ElementsAre())
	AssertThat(t.getFsyncs(), ElementsAre("taco", "tacos"))
}

func (t *FlushFSTest) FsyncError() {
	var err error

	// Open the file.
	t.f1, err = os.OpenFile(path.Join(t.Dir, "foo"), os.O_RDWR, 0)
	AssertEq(nil, err)

	// Configure an fsync error.
	t.setFsyncError(fuse.ENOENT)

	// Fsync.
	err = t.f1.Sync()

	AssertNe(nil, err)
	ExpectThat(err, Error(HasSubstr("no such file")))
}

func (t *FlushFSTest) Dup() {
	var n int
	var err error

	isDarwin := runtime.GOOS == "darwin"
	var expectedFlushes []interface{}

	// Open the file.
	t.f1, err = os.OpenFile(path.Join(t.Dir, "foo"), os.O_WRONLY, 0)
	AssertEq(nil, err)

	fd1 := t.f1.Fd()

	// Use dup(2) to get another copy.
	fd2, err := syscall.Dup(int(fd1))
	AssertEq(nil, err)

	t.f2 = os.NewFile(uintptr(fd2), t.f1.Name())

	// Write some contents with each handle.
	n, err = t.f1.Write([]byte("taco"))
	AssertEq(nil, err)
	AssertEq(4, n)

	n, err = t.f2.Write([]byte("s"))
	AssertEq(nil, err)
	AssertEq(1, n)

	// At this point, no flushes or fsyncs should have happened.
	AssertThat(t.getFlushes(), ElementsAre())
	AssertThat(t.getFsyncs(), ElementsAre())

	// Close one handle. On Linux the current contents should be flushed. On OS
	// X, where the semantics of handles are different, they apparently are not.
	// (Cf. https://github.com/osxfuse/osxfuse/issues/199)
	err = t.f1.Close()
	t.f1 = nil
	AssertEq(nil, err)

	if !isDarwin {
		expectedFlushes = append(expectedFlushes, "tacos")
	}

	AssertThat(t.getFlushes(), ElementsAre(expectedFlushes...))
	AssertThat(t.getFsyncs(), ElementsAre())

	// Write some more contents via the other handle. Again, no further flushes.
	n, err = t.f2.Write([]byte("!"))
	AssertEq(nil, err)
	AssertEq(1, n)

	AssertThat(t.getFlushes(), ElementsAre(expectedFlushes...))
	AssertThat(t.getFsyncs(), ElementsAre())

	// Close the handle. Now the new contents should be flushed.
	err = t.f2.Close()
	t.f2 = nil
	AssertEq(nil, err)

	expectedFlushes = append(expectedFlushes, "tacos!")
	ExpectThat(t.getFlushes(), ElementsAre(expectedFlushes...))
	ExpectThat(t.getFsyncs(), ElementsAre())
}

func (t *FlushFSTest) Dup_FlushError() {
	var err error

	// Open the file.
	t.f1, err = os.OpenFile(path.Join(t.Dir, "foo"), os.O_WRONLY, 0)
	AssertEq(nil, err)

	fd1 := t.f1.Fd()

	// Use dup(2) to get another copy.
	fd2, err := syscall.Dup(int(fd1))
	AssertEq(nil, err)

	t.f2 = os.NewFile(uintptr(fd2), t.f1.Name())

	// Configure a flush error.
	t.setFlushError(fuse.ENOENT)

	// Close by the first handle. On OS X, where the semantics of file handles
	// are different (cf. https://github.com/osxfuse/osxfuse/issues/199), this
	// does not result in an error.
	err = t.f1.Close()
	t.f1 = nil

	if runtime.GOOS == "darwin" {
		AssertEq(nil, err)
	} else {
		AssertNe(nil, err)
		ExpectThat(err, Error(HasSubstr("no such file")))
	}

	// Close by the second handle.
	err = t.f2.Close()
	t.f2 = nil

	AssertNe(nil, err)
	ExpectThat(err, Error(HasSubstr("no such file")))
}

func (t *FlushFSTest) Dup2() {
	var n int
	var err error

	// Open the file.
	t.f1, err = os.OpenFile(path.Join(t.Dir, "foo"), os.O_WRONLY, 0)
	AssertEq(nil, err)

	// Write some contents to the file.
	n, err = t.f1.Write([]byte("taco"))
	AssertEq(nil, err)
	AssertEq(4, n)

	// Open and unlink some temporary file.
	t.f2, err = ioutil.TempFile("", "")
	AssertEq(nil, err)

	err = os.Remove(t.f2.Name())
	AssertEq(nil, err)

	// Duplicate the temporary file descriptor on top of the file from our file
	// system. We should see a flush.
	err = dup2(int(t.f2.Fd()), int(t.f1.Fd()))
	ExpectEq(nil, err)

	ExpectThat(t.getFlushes(), ElementsAre("taco"))
	ExpectThat(t.getFsyncs(), ElementsAre())
}

func (t *FlushFSTest) Dup2_FlushError() {
	var err error

	// Open the file.
	t.f1, err = os.OpenFile(path.Join(t.Dir, "foo"), os.O_WRONLY, 0)
	AssertEq(nil, err)

	// Open and unlink some temporary file.
	t.f2, err = ioutil.TempFile("", "")
	AssertEq(nil, err)

	err = os.Remove(t.f2.Name())
	AssertEq(nil, err)

	// Configure a flush error.
	t.setFlushError(fuse.ENOENT)

	// Duplicate the temporary file descriptor on top of the file from our file
	// system. We shouldn't see the flush error.
	err = dup2(int(t.f2.Fd()), int(t.f1.Fd()))
	ExpectEq(nil, err)
}

func (t *FlushFSTest) Mmap_MunmapBeforeClose() {
	var n int
	var err error

	// If we run this test with GOMAXPROCS=1 (the default), the program will
	// deadlock for the reason described here:
	//
	//     https://groups.google.com/d/msg/golang-nuts/11rdExWP6ac/TzwT6HBOb3wJ
	//
	// In summary, the goroutine reading from the mmap'd file is camping on a
	// scheduler slot while it blocks on a page fault, and the goroutine handling
	// fuse requests is waiting for the scheduler slot.
	//
	// So run with GOMAXPROCS=2.
	old := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(old)

	// Open the file.
	t.f1, err = os.OpenFile(path.Join(t.Dir, "foo"), os.O_RDWR, 0)
	AssertEq(nil, err)

	// Write some contents to the file.
	n, err = t.f1.Write([]byte("taco"))
	AssertEq(nil, err)
	AssertEq(4, n)

	// mmap the file.
	data, err := syscall.Mmap(
		int(t.f1.Fd()), 0, 4,
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_SHARED)

	AssertEq(nil, err)
	AssertEq("taco", string(data))

	// Modify then unmap.
	data[0] = 'p'

	err = syscall.Munmap(data)
	AssertEq(nil, err)

	// munmap does not cause a flush.
	ExpectThat(t.getFlushes(), ElementsAre())
	ExpectThat(t.getFsyncs(), ElementsAre())

	// Close the file. We should see a flush. On Darwin, this will contain out of
	// date contents (cf. https://github.com/osxfuse/osxfuse/issues/202).
	err = t.f1.Close()
	t.f1 = nil
	AssertEq(nil, err)

	if runtime.GOOS == "darwin" {
		ExpectThat(t.getFlushes(), ElementsAre("taco"))
		ExpectThat(t.getFsyncs(), ElementsAre())
	} else {
		ExpectThat(t.getFlushes(), ElementsAre("paco"))
		ExpectThat(t.getFsyncs(), ElementsAre())
	}
}

func (t *FlushFSTest) Mmap_CloseBeforeMunmap() {
	var n int
	var err error

	// If we run this test with GOMAXPROCS=1 (the default), the program will
	// deadlock for the reason described here:
	//
	//     https://groups.google.com/d/msg/golang-nuts/11rdExWP6ac/TzwT6HBOb3wJ
	//
	// In summary, the goroutine reading from the mmap'd file is camping on a
	// scheduler slot while it blocks on a page fault, and the goroutine handling
	// fuse requests is waiting for the scheduler slot.
	//
	// So run with GOMAXPROCS=2.
	old := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(old)

	// Open the file.
	t.f1, err = os.OpenFile(path.Join(t.Dir, "foo"), os.O_RDWR, 0)
	AssertEq(nil, err)

	// Write some contents to the file.
	n, err = t.f1.Write([]byte("taco"))
	AssertEq(nil, err)
	AssertEq(4, n)

	// mmap the file.
	data, err := syscall.Mmap(
		int(t.f1.Fd()), 0, 4,
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_SHARED)

	AssertEq(nil, err)
	AssertEq("taco", string(data))

	// Close the file. We should see a flush.
	err = t.f1.Close()
	t.f1 = nil
	AssertEq(nil, err)

	AssertThat(t.getFlushes(), ElementsAre("taco"))
	AssertThat(t.getFsyncs(), ElementsAre())

	// Modify then unmap.
	data[0] = 'p'

	err = syscall.Munmap(data)
	AssertEq(nil, err)

	// munmap does not cause a flush.
	ExpectThat(t.getFlushes(), ElementsAre("taco"))
	ExpectThat(t.getFsyncs(), ElementsAre())
}

func (t *FlushFSTest) Directory() {
	AssertTrue(false, "TODO")
}

////////////////////////////////////////////////////////////////////////
// No errors
////////////////////////////////////////////////////////////////////////

type NoErrorsTest struct {
	flushFSTest
}

func init() { RegisterTestSuite(&NoErrorsTest{}) }

////////////////////////////////////////////////////////////////////////
// Flush error
////////////////////////////////////////////////////////////////////////

type FlushErrorTest struct {
	flushFSTest
}

func init() { RegisterTestSuite(&FlushErrorTest{}) }

func (t *FlushFSTest) SetUp(ti *TestInfo)

////////////////////////////////////////////////////////////////////////
// Fsync  error
////////////////////////////////////////////////////////////////////////

type FsyncErrorTest struct {
	flushFSTest
}

func init() { RegisterTestSuite(&FsyncErrorTest{}) }

func (t *FlushFSTest) SetUp(ti *TestInfo)
