package power

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/pwr_mgt/IOPMLib.h>
#include <IOKit/ps/IOPowerSources.h>
#include <IOKit/ps/IOPSKeys.h>

static int fuyuOnAC(void) {
    CFTypeRef info = IOPSCopyPowerSourcesInfo();
    if (!info) return -1;
    CFStringRef source = IOPSGetProvidingPowerSourceType(info);
    int result = source ? CFEqual(source, CFSTR(kIOPSACPowerValue)) : -1;
    CFRelease(info);
    return result;
}

static IOReturn fuyuKeepAwake(IOPMAssertionID *identifier) {
    return IOPMAssertionCreateWithName(kIOPMAssertionTypePreventUserIdleSystemSleep,
        kIOPMAssertionLevelOn, CFSTR("Fuyu: keep Mac awake"), identifier);
}
*/
import "C"

import "fmt"

type systemBackend struct {
	identifier C.IOPMAssertionID
}

func (driver *systemBackend) onAC() (bool, error) {
	result := C.fuyuOnAC()
	if result < 0 {
		return false, fmt.Errorf("无法读取 Mac 电源状态")
	}
	return result != 0, nil
}

func (driver *systemBackend) acquire() error {
	if result := C.fuyuKeepAwake(&driver.identifier); result != C.kIOReturnSuccess {
		return fmt.Errorf("macOS 电源断言创建失败 (%d)", int(result))
	}
	return nil
}

func (driver *systemBackend) release() error {
	if result := C.IOPMAssertionRelease(driver.identifier); result != C.kIOReturnSuccess {
		return fmt.Errorf("macOS 电源断言释放失败 (%d)", int(result))
	}
	return nil
}
