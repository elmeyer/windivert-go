package windivert

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const winDivertDLLName = "WinDivert.dll"

const addrSize = unsafe.Sizeof(Address{})

var (
	loadOnce     sync.Once
	winDivertDLL *windows.DLL

	winDivertOpen              *windows.Proc
	winDivertRecv              *windows.Proc
	winDivertRecvEx            *windows.Proc
	winDivertSend              *windows.Proc
	winDivertSendEx            *windows.Proc
	winDivertShutdown          *windows.Proc
	winDivertClose             *windows.Proc
	winDivertSetParam          *windows.Proc
	winDivertGetParam          *windows.Proc
	winDivertHelperParsePacket *windows.Proc
)

func loadDLL() {
	winDivertDLL = windows.MustLoadDLL(winDivertDLLName)

	winDivertOpen = winDivertDLL.MustFindProc("WinDivertOpen")
	winDivertRecv = winDivertDLL.MustFindProc("WinDivertRecv")
	winDivertRecvEx = winDivertDLL.MustFindProc("WinDivertRecvEx")
	winDivertSend = winDivertDLL.MustFindProc("WinDivertSend")
	winDivertSendEx = winDivertDLL.MustFindProc("WinDivertSendEx")
	winDivertShutdown = winDivertDLL.MustFindProc("WinDivertShutdown")
	winDivertClose = winDivertDLL.MustFindProc("WinDivertClose")
	winDivertSetParam = winDivertDLL.MustFindProc("WinDivertSetParam")
	winDivertGetParam = winDivertDLL.MustFindProc("WinDivertGetParam")
	winDivertHelperParsePacket = winDivertDLL.MustFindProc("WinDivertHelperParsePacket")
}

type Handle uintptr

// Open returns windivert Handle if succeed.
func Open(filter string, layer Layer, priority int16, flags uint64) (Handle, error) {
	loadOnce.Do(loadDLL) /* make sure DLL only load once. */

	if priority < PriorityLowest || priority > PriorityHighest {
		return 0, fmt.Errorf("invalid priority (%d)", priority)
	}

	filterPtr, err := windows.BytePtrFromString(filter)
	if err != nil {
		return 0, err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r1, _, err := winDivertOpen.Call(
		uintptr(unsafe.Pointer(filterPtr)),
		uintptr(layer),
		uintptr(priority),
		uintptr(flags),
	)

	if windows.Handle(r1) == windows.InvalidHandle {
		return 0, Error(err.(windows.Errno))
	}

	return Handle(r1), nil
}

func (h Handle) Recv(packet []byte, address *Address) (int, error) {
	var recvLen uint
	ok, _, err := winDivertRecv.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&packet[0])),
		uintptr(len(packet)),
		uintptr(unsafe.Pointer(&recvLen)),
		uintptr(unsafe.Pointer(address)),
	)

	if ok == 0 {
		return 0, Error(err.(windows.Errno))
	}

	return int(recvLen), nil
}

func (h Handle) RecvEx(packets []byte, addresses []Address, addrLen *uint32, overlapped *windows.Overlapped) error {
	if addrLen == nil {
		return errors.New("parameter is nil")
	}
	*addrLen = uint32(uintptr(len(addresses)) * addrSize)

	ok, _, err := winDivertRecvEx.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&packets[0])),
		uintptr(len(packets)),
		uintptr(0),
		uintptr(0),
		uintptr(unsafe.Pointer(&addresses[0])),
		uintptr(unsafe.Pointer(addrLen)),
		uintptr(unsafe.Pointer(overlapped)),
	)

	if ok == 0 {
		return Error(err.(windows.Errno))
	}

	return nil
}

func (h Handle) Send(packet []byte, address *Address) (int, error) {
	var sendLen uint
	ok, _, err := winDivertSend.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&packet[0])),
		uintptr(len(packet)),
		uintptr(unsafe.Pointer(&sendLen)),
		uintptr(unsafe.Pointer(address)),
	)

	if ok == 0 {
		return 0, Error(err.(windows.Errno))
	}

	return int(sendLen), nil
}

func (h Handle) SendEx(packets []byte, addresses []Address) (int, error) {
	var sendLen uint
	addrLen := uintptr(len(addresses)) * addrSize

	ok, _, err := winDivertSendEx.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&packets[0])),
		uintptr(len(packets)),
		uintptr(unsafe.Pointer(&sendLen)),
		uintptr(0),
		uintptr(unsafe.Pointer(&addresses[0])),
		addrLen,
		uintptr(0),
	)

	if ok == 0 {
		return 0, Error(err.(windows.Errno))
	}

	return int(sendLen), nil
}

func (h Handle) Shutdown(how Shutdown) error {
	ok, _, err := winDivertShutdown.Call(
		uintptr(h),
		uintptr(how),
	)

	if ok == 0 {
		return Error(err.(windows.Errno))
	}

	return nil
}

func (h Handle) Close() error {
	ok, _, err := winDivertClose.Call(uintptr(h))

	if ok == 0 {
		return Error(err.(windows.Errno))
	}

	return nil
}

func (h Handle) SetParam(param Param, value uint64) error {
	ok, _, err := winDivertSetParam.Call(
		uintptr(h),
		uintptr(param),
		uintptr(value),
	)

	if ok == 0 {
		return Error(err.(windows.Errno))
	}

	return nil
}

func (h Handle) GetParam(param Param) (uint64, error) {
	var value uint64
	ok, _, err := winDivertGetParam.Call(
		uintptr(h),
		uintptr(param),
		uintptr(unsafe.Pointer(&value)),
	)

	if ok == 0 {
		return 0, Error(err.(windows.Errno))
	}

	return value, nil
}

// HelperParsePacket returns the length of the first packet in packets.
// Loop over a batch of packets by first calling this function on the full
// batch, returning the length of the first packet, and then calling it again
// on packets[previousPacketLen:] and so on.
func HelperParsePacket(packets []byte) (int, error) {
	var pNextLen uint

	ok, _, err := winDivertHelperParsePacket.Call(
		uintptr(unsafe.Pointer(&packets[0])),
		uintptr(len(packets)),
		uintptr(0), // ppIpHdr
		uintptr(0), // ppIpv6Hdr
		uintptr(0), // pProtocol
		uintptr(0), // ppIcmpHdr
		uintptr(0), // ppIcmpv6Hdr
		uintptr(0), // ppTcpHdr
		uintptr(0), // ppUdpHdr
		uintptr(0), // ppData
		uintptr(0), // pDataLen
		uintptr(0), // ppNext
		uintptr(unsafe.Pointer(&pNextLen)),
	)

	if ok == 0 {
		return 0, Error(err.(windows.Errno))
	}

	return len(packets) - int(pNextLen), nil
}
