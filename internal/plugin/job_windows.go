package plugin

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// job holds a plugin's process tree. Killing it, or closing its last handle (also when sonar
// itself dies), ends every process in it.
type job windows.Handle

// newJob puts process pid in a new kill-on-close job.
// shortcut: a child the plugin starts before this runs escapes the job; starting suspended would close that gap.
func newJob(pid int) (job, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if err == nil {
		var ph windows.Handle
		if ph, err = windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid)); err == nil {
			err = windows.AssignProcessToJobObject(h, ph)
			windows.CloseHandle(ph)
		}
	}
	if err != nil {
		windows.CloseHandle(h)
		return 0, err
	}
	return job(h), nil
}

func (j job) kill() {
	if j != 0 {
		windows.TerminateJobObject(windows.Handle(j), 1)
	}
}

func (j job) close() {
	if j != 0 {
		windows.CloseHandle(windows.Handle(j))
	}
}
