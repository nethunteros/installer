//
// Copyright 2017 The Maru OS Project
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

// Wrapper around a fastboot binary.
//
// Note that fastboot (strangely) writes only to stderr for some actions like
// "getvar" or "oem device-info".

package android

import (
	"strings"
)

type FastbootError struct {
	Output string
	Err    error
}

func (e *FastbootError) Error() string {
	return "fastboot: " + e.Err.Error()
}

func NewFastbootError(output string, err error) *FastbootError {
	return &FastbootError{output, err}
}

type FastbootClient struct {
	BinaryAndroidTool
}

func NewFastbootClient() FastbootClient {
	return FastbootClient{BinaryAndroidTool{"fastboot"}}
}

func (f *FastbootClient) getVar(variable string) (value string, err error) {
	output, err := f.Run("getvar", variable)
	if err != nil {
		return output, NewFastbootError(output, err)
	}

	// fastboot reports "[variable]: [value]\n...\n"
	value = strings.Split(strings.Split(output, FastbootLineSeperator)[0], " ")[1]
	return value, nil
}

func (f *FastbootClient) Status() (AndroidDeviceStatus, error) {
	output, err := f.Run("devices")
	if err != nil {
		return NoDeviceFound, NewFastbootError(output, err)
	}

	if len(output) == 0 {
		return NoDeviceFound, nil
	} else if strings.Contains(output, "no permissions") {
		return NoUsbPerms, nil
	} else {
		return DeviceConnected, nil
	}
}

func (f *FastbootClient) GetProduct() (product string, err error) {
	return f.getVar("product")
}

func (f *FastbootClient) FlashRecovery(image string) (err error) {
	output, err := f.Run("flash", "recovery", image)
	if err != nil {
		return NewFastbootError(output, err)
	}
	return nil
}

func (f *FastbootClient) Boot(image string) (err error) {
	output, err := f.Run("boot", image)
	if err != nil {
		return NewFastbootError(output, err)
	}
	return nil
}

func (f *FastbootClient) Reboot() (err error) {
	output, err := f.Run("reboot")
	if err != nil {
		return NewFastbootError(output, err)
	}
	return nil
}

func (f *FastbootClient) Unlocked() (bool, error) {
	product, err := f.GetProduct()
	if err != nil {
		return false, err
	}

	// flo is a special case since it reports the wrong lock state from oem
	// device-info
	if "flo" == product {
		lockState, err := f.getVar("lock_state")
		return "unlocked" == lockState, err
	}

	deviceInfo, err := f.Run("oem", "device-info")
	if err != nil {
		return false, err
	}

	unlocked := false
	lines := strings.Split(deviceInfo, FastbootLineSeperator)
	for _, line := range lines {
		if strings.Contains(line, "Device unlocked") {
			unlocked = "true" == strings.Split(line, " ")[3]
		}
	}

	return unlocked, err
}

func (f *FastbootClient) Unlock() (err error) {
	output, err := f.Run("oem", "unlock")
	if err != nil {
		return NewFastbootError(output, err)
	}
	return nil
}
