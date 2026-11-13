//go:build windows

// rdpprobe prints the interfaces of the RDP ActiveX control (mstscax.dll)
// from its type library: IIDs and vtable slots of the non-scriptable ones,
// which easyTTY calls directly. A development aid, not part of the app.
package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	oleaut32       = windows.NewLazySystemDLL("oleaut32.dll")
	procLoadTypeLb = oleaut32.NewProc("LoadTypeLibEx")
	procSysFree    = oleaut32.NewProc("SysFreeString")
)

// call invokes slot n of a COM object's vtable.
func call(obj unsafe.Pointer, slot int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, slot*int(unsafe.Sizeof(uintptr(0)))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

// bstr converts and frees a BSTR.
func bstr(p *uint16) string {
	if p == nil {
		return ""
	}
	s := windows.UTF16PtrToString(p)
	procSysFree.Call(uintptr(unsafe.Pointer(p)))
	return s
}

func main() {
	filter := "NonScriptable"
	if len(os.Args) > 1 {
		filter = os.Args[1]
	}
	path, _ := windows.UTF16PtrFromString(`C:\Windows\System32\mstscax.dll`)
	var lib unsafe.Pointer
	if r, _, _ := procLoadTypeLb.Call(uintptr(unsafe.Pointer(path)), 2 /*REGKIND_NONE*/, uintptr(unsafe.Pointer(&lib))); r != 0 {
		fmt.Printf("LoadTypeLibEx: 0x%x\n", r)
		os.Exit(1)
	}
	n := int(call(lib, 3)) // GetTypeInfoCount
	for i := 0; i < n; i++ {
		var name *uint16
		call(lib, 9, uintptr(i), uintptr(unsafe.Pointer(&name)), 0, 0, 0) // GetDocumentation
		iname := bstr(name)
		if !strings.Contains(iname, filter) {
			continue
		}
		var ti unsafe.Pointer
		call(lib, 4, uintptr(i), uintptr(unsafe.Pointer(&ti))) // GetTypeInfo
		var attr unsafe.Pointer                                // TYPEATTR
		call(ti, 3, uintptr(unsafe.Pointer(&attr)))            // GetTypeAttr
		guid := *(*windows.GUID)(attr)
		kind := *(*int32)(unsafe.Add(attr, 44))
		cFuncs := *(*uint16)(unsafe.Add(attr, 48))
		fmt.Printf("%s %s kind=%d funcs=%d\n", iname, guid.String(), kind, cFuncs)
		for f := 0; f < int(cFuncs); f++ {
			var fd unsafe.Pointer                                 // FUNCDESC
			call(ti, 5, uintptr(f), uintptr(unsafe.Pointer(&fd))) // GetFuncDesc
			memid := *(*int32)(fd)
			invkind := *(*int32)(unsafe.Add(fd, 28))
			params := *(*int16)(unsafe.Add(fd, 36))
			oVft := *(*int16)(unsafe.Add(fd, 40))
			var fname *uint16
			var got uint32
			call(ti, 7, uintptr(memid), uintptr(unsafe.Pointer(&fname)), 1, uintptr(unsafe.Pointer(&got))) // GetNames
			fmt.Printf("   slot %2d  invkind=%d  params=%d  %s\n", int(oVft)/8, invkind, params, bstr(fname))
			call(ti, 20, uintptr(fd)) // ReleaseFuncDesc
		}
		call(ti, 19, uintptr(attr)) // ReleaseTypeAttr
		call(ti, 2)
	}
	call(lib, 2)
}
