package app

/*
#include <sys/sysctl.h>
#include <mach/mach_host.h>
#include <mach/mach_init.h>
#include <mach/mach_error.h>
#include <mach/vm_map.h>


// Wrapper for host_statistics64
kern_return_t get_vm_statistics(vm_statistics64_data_t *vm_stat) {
    mach_msg_type_number_t count = HOST_VM_INFO64_COUNT;
    return host_statistics64(mach_host_self(), HOST_VM_INFO64, (host_info64_t)vm_stat, &count);
}
*/
import "C"
import (
	"fmt"
	"unsafe"
)

type NativeMemoryMetrics struct {
	Total     uint64
	Used      uint64
	Available uint64
	SwapTotal uint64
	SwapUsed  uint64
}

var (
	pageSize    uint64
	totalMemory uint64
)

func initNativeStats() error {
	// Get page size
	var size C.size_t = C.sizeof_int
	var pSize C.int
	if C.sysctlbyname(C.CString("hw.pagesize"), unsafe.Pointer(&pSize), &size, nil, 0) != 0 {
		return fmt.Errorf("failed to get page size")
	}
	pageSize = uint64(pSize)

	// Get total memory
	var mSize C.uint64_t
	size = C.sizeof_uint64_t
	if C.sysctlbyname(C.CString("hw.memsize"), unsafe.Pointer(&mSize), &size, nil, 0) != 0 {
		return fmt.Errorf("failed to get memsize")
	}
	totalMemory = uint64(mSize)
	return nil
}

func GetNativeMemoryMetrics() (NativeMemoryMetrics, error) {
	if totalMemory == 0 {
		if err := initNativeStats(); err != nil {
			return NativeMemoryMetrics{}, err
		}
	}

	var vmStat C.vm_statistics64_data_t
	if ret := C.get_vm_statistics(&vmStat); ret != C.KERN_SUCCESS {
		return NativeMemoryMetrics{}, fmt.Errorf("failed to get vm statistics: %d", ret)
	}

	free := uint64(vmStat.free_count) * pageSize
	// active := uint64(vmStat.active_count) * pageSize
	inactive := uint64(vmStat.inactive_count) * pageSize
	// wired := uint64(vmStat.wire_count) * pageSize
	// compressed := uint64(vmStat.compressor_page_count) * pageSize

	available := free + inactive
	used := totalMemory - available

	// Swap
	var xsw C.struct_xsw_usage
	size := C.size_t(C.sizeof_struct_xsw_usage)
	if C.sysctlbyname(C.CString("vm.swapusage"), unsafe.Pointer(&xsw), &size, nil, 0) != 0 {
		// Swap might be disabled or failed, just return 0s
		return NativeMemoryMetrics{
			Total:     totalMemory,
			Used:      used,
			Available: available,
			SwapTotal: 0,
			SwapUsed:  0,
		}, nil
	}

	return NativeMemoryMetrics{
		Total:     totalMemory,
		Used:      used,
		Available: available,
		SwapTotal: uint64(xsw.xsu_total),
		SwapUsed:  uint64(xsw.xsu_used),
	}, nil
}
