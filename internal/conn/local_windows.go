//go:build windows

package conn

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procUpdateProcThreadAttribute = windows.NewLazySystemDLL("kernel32.dll").NewProc("UpdateProcThreadAttribute")

// LocalConn is a shell on this PC running in a Windows pseudo console (ConPTY).
type LocalConn struct {
	in     *os.File      // keyboard → shell
	out    *os.File      // shell → screen
	exited chan struct{} // closed once the shell has ended

	procMu     sync.Mutex // proc is closed by waitExit only, after the wait
	proc       windows.Handle
	procClosed bool

	mu       sync.Mutex
	pc       windows.Handle
	pcClosed bool
	once     sync.Once
}

// OpenLocal starts the shell in a pseudo console of cols × rows.
func OpenLocal(cfg LocalConfig) (*LocalConn, error) {
	exe, cmdline, err := shellCommand(cfg.Shell, os.Getenv("SystemRoot"), exec.LookPath)
	if err != nil {
		return nil, err
	}
	dir, err := startDir(cfg.Dir, cfg.Home)
	if err != nil {
		return nil, err
	}
	var ptyIn, inW, outR, ptyOut windows.Handle
	if err := windows.CreatePipe(&ptyIn, &inW, nil, 0); err != nil {
		return nil, fmt.Errorf("Pipe: %w", err)
	}
	if err := windows.CreatePipe(&outR, &ptyOut, nil, 0); err != nil {
		closeHandles(ptyIn, inW)
		return nil, fmt.Errorf("Pipe: %w", err)
	}
	var pc windows.Handle
	err = windows.CreatePseudoConsole(coord(cfg.Cols, cfg.Rows), ptyIn, ptyOut, 0, &pc)
	closeHandles(ptyIn, ptyOut) // the pseudo console holds its own copies
	if err != nil {
		closeHandles(inW, outR)
		return nil, fmt.Errorf("Pseudo-Konsole nicht verfügbar (ab Windows 10 1809): %w", err)
	}
	proc, err := startInConsole(pc, exe, cmdline, dir)
	if err != nil {
		windows.ClosePseudoConsole(pc)
		closeHandles(inW, outR)
		return nil, err
	}
	c := &LocalConn{in: os.NewFile(uintptr(inW), "conpty-in"), out: os.NewFile(uintptr(outR), "conpty-out"), proc: proc, pc: pc, exited: make(chan struct{})}
	go c.waitExit()
	return c, nil
}

func startInConsole(pc windows.Handle, exe, cmdline, dir string) (windows.Handle, error) {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return 0, err
	}
	defer attrs.Delete()
	// The value is the console handle itself, not a pointer to it.
	r, _, callErr := procUpdateProcThreadAttribute.Call(uintptr(unsafe.Pointer(attrs.List())), 0,
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, uintptr(pc), unsafe.Sizeof(pc), 0, 0)
	if r == 0 {
		return 0, fmt.Errorf("Pseudo-Konsole zuweisen: %w", callErr)
	}
	si := windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(si))
	// No inherited standard handles: the shell must talk only to the pseudo console.
	si.Flags = windows.STARTF_USESTDHANDLES
	exe16, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return 0, err
	}
	cmd16, err := windows.UTF16PtrFromString(cmdline)
	if err != nil {
		return 0, err
	}
	dir16, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var pi windows.ProcessInformation
	err = windows.CreateProcess(exe16, cmd16, nil, nil, false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT, nil, dir16, &si.StartupInfo, &pi)
	if err != nil {
		return 0, fmt.Errorf("%s starten: %w", exe, err)
	}
	_ = windows.CloseHandle(pi.Thread)
	return pi.Process, nil
}

// waitExit closes the pseudo console once the shell has ended ("exit"), so
// the output pipe reaches its end and the tab shows the session as closed.
func (c *LocalConn) waitExit() {
	_, _ = windows.WaitForSingleObject(c.proc, windows.INFINITE)
	close(c.exited)
	c.procMu.Lock()
	c.procClosed = true
	_ = windows.CloseHandle(c.proc)
	c.procMu.Unlock()
	c.closeConsole()
}

func (c *LocalConn) closeConsole() {
	c.mu.Lock()
	if c.pcClosed {
		c.mu.Unlock()
		return
	}
	c.pcClosed = true // Resize stops touching the console from here on
	c.mu.Unlock()
	// Older Windows versions return only after the last output was read
	// (the tab's reader or Close's drain takes it): not under the lock.
	windows.ClosePseudoConsole(c.pc)
}

func (c *LocalConn) Read(p []byte) (int, error)  { return c.out.Read(p) }
func (c *LocalConn) Write(p []byte) (int, error) { return c.in.Write(p) }

// Resize follows the tab size.
func (c *LocalConn) Resize(cols, rows int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pcClosed {
		return nil
	}
	return windows.ResizePseudoConsole(c.pc, coord(cols, rows))
}

// Close ends the shell (closing the console ends its processes) without
// blocking: whatever output is left is read and dropped in the background.
func (c *LocalConn) Close() error {
	c.once.Do(func() {
		_ = c.in.Close()
		go func() {
			_, _ = io.Copy(io.Discard, c.out)
			_ = c.out.Close()
		}()
		go func() {
			c.closeConsole()
			select {
			case <-c.exited:
			case <-time.After(closeWait):
				c.procMu.Lock()
				if !c.procClosed {
					_ = windows.TerminateProcess(c.proc, 1) // a shell that ignored the close
				}
				c.procMu.Unlock()
			}
		}()
	})
	return nil
}

const closeWait = 3 * time.Second

func coord(cols, rows int) windows.Coord {
	return windows.Coord{X: int16(clampDim(cols, 80)), Y: int16(clampDim(rows, 24))}
}

// clampDim keeps a size the pseudo console accepts.
func clampDim(v, def int) int {
	if v <= 0 {
		return def
	}
	return min(v, 1000)
}

func closeHandles(hs ...windows.Handle) {
	for _, h := range hs {
		_ = windows.CloseHandle(h)
	}
}
