//go:build windows

package shared

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	cchRmSessionKey = 32
	cchRmMaxAppName = 255
	cchRmMaxSvcName = 63
	errorMoreData   = 234
)

type rmUniqueProcess struct {
	ProcessID uint32
	StartTime windows.Filetime
}

type rmProcessInfo struct {
	Process          rmUniqueProcess
	AppName          [cchRmMaxAppName + 1]uint16
	ServiceShortName [cchRmMaxSvcName + 1]uint16
	ApplicationType  uint32
	AppStatus        uint32
	SessionID        uint32
	Restartable      uint32
}

var (
	restartManagerDLL   = windows.NewLazySystemDLL("rstrtmgr.dll")
	rmStartSession      = restartManagerDLL.NewProc("RmStartSession")
	rmRegisterResources = restartManagerDLL.NewProc("RmRegisterResources")
	rmGetList           = restartManagerDLL.NewProc("RmGetList")
	rmEndSession        = restartManagerDLL.NewProc("RmEndSession")
)

func lockingProcesses(path string) ([]FileLockingProcess, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("encode file path: %w", err)
	}

	var session uint32
	var sessionKey [cchRmSessionKey + 1]uint16
	if err := rmCall("start session", rmStartSession, uintptr(unsafe.Pointer(&session)), 0, uintptr(unsafe.Pointer(&sessionKey[0]))); err != nil {
		return nil, err
	}
	defer rmEndSession.Call(uintptr(session))

	files := []*uint16{pathPtr}
	if err := rmCall("register file", rmRegisterResources,
		uintptr(session),
		uintptr(len(files)),
		uintptr(unsafe.Pointer(&files[0])),
		0,
		0,
		0,
		0,
	); err != nil {
		return nil, err
	}

	return rmAffectedProcesses(session)
}

func rmAffectedProcesses(session uint32) ([]FileLockingProcess, error) {
	for attempt := 0; attempt < 3; attempt++ {
		var needed uint32
		var rebootReasons uint32
		result, _, _ := rmGetList.Call(
			uintptr(session),
			uintptr(unsafe.Pointer(&needed)),
			0,
			0,
			uintptr(unsafe.Pointer(&rebootReasons)),
		)
		if result == 0 && needed == 0 {
			return nil, nil
		}
		if result != errorMoreData && result != 0 {
			return nil, rmResultError("get process count", result)
		}
		if needed == 0 {
			return nil, nil
		}

		processes := make([]rmProcessInfo, needed)
		count := needed
		result, _, _ = rmGetList.Call(
			uintptr(session),
			uintptr(unsafe.Pointer(&needed)),
			uintptr(unsafe.Pointer(&count)),
			uintptr(unsafe.Pointer(&processes[0])),
			uintptr(unsafe.Pointer(&rebootReasons)),
		)
		if result == errorMoreData {
			continue
		}
		if result != 0 {
			return nil, rmResultError("get process list", result)
		}

		locking := make([]FileLockingProcess, 0, count)
		for _, process := range processes[:count] {
			locking = append(locking, FileLockingProcess{
				PID:         int(process.Process.ProcessID),
				Name:        syscall.UTF16ToString(process.AppName[:]),
				ServiceName: syscall.UTF16ToString(process.ServiceShortName[:]),
				Restartable: process.Restartable != 0,
			})
		}
		return locking, nil
	}

	return nil, fmt.Errorf("process list changed while querying the file with Restart Manager")
}

func rmCall(operation string, procedure *windows.LazyProc, arguments ...uintptr) error {
	result, _, _ := procedure.Call(arguments...)
	if result != 0 {
		return rmResultError(operation, result)
	}
	return nil
}

func rmResultError(operation string, result uintptr) error {
	return fmt.Errorf("restart manager %s: %w", operation, syscall.Errno(result))
}
