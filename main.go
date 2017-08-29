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

package main

import (
	"bufio"
	"flag"
	"fmt"
	"github.com/nethunteros/installer/android"
	"github.com/nethunteros/installer/remote"
	"github.com/pdsouza/toolbox.go/ui"
	"os"
	"path"
	"runtime"
	"time"
)

const (
	// Success exit codes.
	SuccessBase = 1<<5 + iota
	SuccessUserAbort
	SuccessBootloaderUnlocked

	Success = 0
)

const (
	// Error exit codes.
	ErrorBase = 1<<6 + iota
	ErrorPrereqs
	ErrorUserInput
	ErrorUsbPerms
	ErrorAdb
	ErrorFastboot
	ErrorRemote
	ErrorTWRP
)

var (
	reader      = bufio.NewReader(os.Stdin)
	progressBar = ui.ProgressBar{0, 10, ""}
)

func iEcho(format string, a ...interface{}) {
	fmt.Printf(format+"\n", a...)
}

func eEcho(msg string) {
	iEcho(msg)
}

func verifyAdbStatusOrAbort(adb *android.AdbClient) {
	status, err := adb.Status()
	if err != nil {
		eEcho("Failed to get adb status: " + err.Error())
		exit(ErrorAdb)
	}
	if status == android.NoDeviceFound || status == android.DeviceUnauthorized {
		eEcho(MsgAdbIssue)
		exit(ErrorAdb)
	} else if status == android.NoUsbPerms {
		eEcho(MsgFixPerms)
		exit(ErrorUsbPerms)
	}
}

func verifyFastbootStatusOrAbort(fastboot *android.FastbootClient) {
	status, err := fastboot.Status()
	if err != nil {
		eEcho("Failed to get fastboot status: " + err.Error())
		exit(ErrorFastboot)
	}
	if status == android.NoDeviceFound {
		eEcho(MsgFastbootNoDeviceFound)
		exit(ErrorFastboot)
	} else if status == android.NoUsbPerms {
		eEcho(MsgFixPerms)
		exit(ErrorUsbPerms)
	}
}

func progressCallback(percent float64) {
	progressBar.Progress = percent
	fmt.Print("\r" + progressBar.Render())
	if percent == 1.0 {
		fmt.Println()
	}
}

func exit(code int) {
	// When run by double-clicking the executable on windows, the command
	// prompt will immediately exit upon program completion, making it hard for
	// users to see the last few messages. Let's explicitly wait for
	// acknowledgement from the user.
	if runtime.GOOS == "windows" {
		fmt.Print("\nPress [Enter] to exit...")
		reader.ReadLine() // pause until the user presses enter
	}

	os.Exit(code)
}

func main() {

	/*
	Step 1 - Set path to binaries
	Step 2 - Verify ADB and Fastboot
	Step 3 - Check USB permissions
	Step 4 - Identify this is the correct device
	Step 5 - Detect if device is unlocked, then unlock
	Step 6 - Download: Nethunter, Oxygen Recovery, Oxygen Factory, TWRP Recovery
	Step 7 - Boot into Oxygen Recovery
	Step 8 - Reflash factory

	*/

	var versionFlag = flag.Bool("version", false, "print the program version")
	flag.Parse()
	if *versionFlag == true {
		iEcho("Nethunter installer version %s %s/%s", Version, runtime.GOOS, runtime.GOARCH)
		exit(Success)
	}

	myPath, err := os.Executable()
	if err != nil {
		panic(err)
	}

	// include any bundled binaries in PATH
	err = os.Setenv("PATH", path.Dir(myPath)+":"+os.Getenv("PATH"))
	if err != nil {
		eEcho("Failed to set PATH to include installer tools: " + err.Error())
		exit(ErrorPrereqs)
	}

	// try to use the installer dir as the workdir to make sure any temporary
	// files or downloaded dependencies are isolated to the installer dir
	if err = os.Chdir(path.Dir(myPath)); err != nil {
		eEcho("Warning: failed to change working directory")
	}

	iEcho(MsgWelcome)

	fmt.Print("Are you ready to install Nethunter? (yes/no): ")
	responseBytes, _, err := reader.ReadLine()
	if err != nil {
		iEcho("Failed to read input: ", err.Error())
		exit(ErrorUserInput)
	}

	if "yes" != string(responseBytes) {
		iEcho("")
		iEcho("Aborting installation.")
		exit(SuccessUserAbort)
	}

	iEcho("")
	iEcho("Verifying installer tools...")
	adb := android.NewAdbClient()
	if _, err := adb.Status(); err != nil {
		eEcho("Failed to run adb: " + err.Error())
		eEcho(MsgIncompleteZip)
		exit(ErrorPrereqs)
	}

	fastboot := android.NewFastbootClient()
	if _, err := fastboot.Status(); err != nil {
		eEcho("Failed to run fastboot: " + err.Error())
		eEcho(MsgIncompleteZip)
		exit(ErrorPrereqs)
	}

	iEcho("Checking USB permissions...")
	status, _ := fastboot.Status()
	if status == android.NoDeviceFound {
		// We are in ADB mode (normal boot or recovery).

		verifyAdbStatusOrAbort(&adb)

		iEcho("Rebooting your device into bootloader...")
		err = adb.Reboot("bootloader")
		if err != nil {
			eEcho("Failed to reboot into bootloader: " + err.Error())
			exit(ErrorAdb)
		}

		time.Sleep(7000 * time.Millisecond)

		if status, err = fastboot.Status(); err != nil || status == android.NoDeviceFound {
			eEcho("Failed to reboot device into bootloader!")
			exit(ErrorAdb)
		}
	}

	// We are in fastboot mode (the bootloader).

	verifyFastbootStatusOrAbort(&fastboot)

	iEcho("Identifying your device...")
	product, err := fastboot.GetProduct()

	if err != nil {
		eEcho("Failed to get device product info: " + err.Error())
		exit(ErrorFastboot)
	}

	// OnePlus references there phones as below
	if "QC_Reference_Phone" == product {
		// Assume this is a cheeseburger (OnePlus5)
		product := "cheeseburger"
	}

	unlocked, err := fastboot.Unlocked()
	if err != nil {
		iEcho("Warning: unable to determine bootloader lock state: " + err.Error())
	}
	if !unlocked {
		iEcho("Unlocking bootloader, you will need to confirm this on your device...")
		err = fastboot.Unlock()
		if err != nil {
			eEcho("Failed to unlock bootloader: " + err.Error())
			exit(ErrorFastboot)
		}
		fastboot.Reboot()
		iEcho(MsgUnlockSuccess)
		exit(SuccessBootloaderUnlocked)
	}

	// This needs to be replaces with our zip file
	iEcho("Downloading the latest release for your device (%q)...", product)
	req, err = remote.RequestNethunter()
	if err != nil {
		eEcho("Failed to download Nethunter: " + err.Error())
		exit(ErrorRemote)
	}

	zip := req.Filename
	if _, err = os.Stat(zip); os.IsNotExist(err) { // skip if we already downloaded it
		progressBar.Title = zip
		req.ProgressHandler = func(percent float64) {
			progressBar.Progress = percent
			fmt.Print("\r" + progressBar.Render())
			if percent == 1.0 {
				fmt.Println()
			}
		}
		zip, err = req.Download()
		if err != nil {
			eEcho("") // extra newline in case progress bar didn't finish
			eEcho("Failed to download Nethunter: " + err.Error())
			exit(ErrorRemote)
		}
	}

	iEcho("Downloading TWRP for your device...")
	req, err = remote.RequestTWRP(product)
	if err != nil {
		eEcho("Failed to request TWRP: " + err.Error())
		exit(ErrorRemote)
	}

	twrp := req.Filename
	if _, err = os.Stat(twrp); os.IsNotExist(err) { // skip if we already downloaded it
		progressBar.Title = twrp
		req.ProgressHandler = func(percent float64) {
			progressBar.Progress = percent
			fmt.Print("\r" + progressBar.Render())
			if percent == 1.0 {
				fmt.Println()
			}
		}
		twrp, err = req.Download()
		if err != nil {
			eEcho("") // extra newline in case progress bar didn't finish
			eEcho("Failed to download TWRP: " + err.Error())
			exit(ErrorRemote)
		}
	}

	iEcho("Downloading OnePlus5 OxygenOS recovery for your device...")
	req, err = remote.RequestOxygenRecovery()
	if err != nil {
		eEcho("Failed to request TWRP: " + err.Error())
		exit(ErrorRemote)
	}

	oxygenrecovery := req.Filename
	if _, err = os.Stat(twrp); os.IsNotExist(err) { // skip if we already downloaded it
		progressBar.Title = oxygenrecovery
		req.ProgressHandler = func(percent float64) {
			progressBar.Progress = percent
			fmt.Print("\r" + progressBar.Render())
			if percent == 1.0 {
				fmt.Println()
			}
		}
		oxygenrecovery, err = req.Download()
		if err != nil {
			eEcho("") // extra newline in case progress bar didn't finish
			eEcho("Failed to download Oxygen Recovery: " + err.Error())
			exit(ErrorRemote)
		}
	}

	// Download Factory image
	iEcho("Downloading OnePlus 5 factory for your device...")
	req, err = remote.RequestFactory()
	if err != nil {
		eEcho("Failed to request OnePlus 5 Factory image: " + err.Error())
		exit(ErrorRemote)
	}

	factory := req.Filename
	if _, err = os.Stat(factory); os.IsNotExist(err) { 	// err = file is already downloaded
		progressBar.Title = factory
		req.ProgressHandler = func(percent float64) {
			progressBar.Progress = percent
			fmt.Print("\r" + progressBar.Render())
			if percent == 1.0 {
				fmt.Println()
			}
		}
		factory, err = req.Download()
		if err != nil {
			eEcho("") // extra newline in case progress bar didn't finish
			eEcho("Failed to download factory image: " + err.Error())
			exit(ErrorRemote)
		}
	}

	// Boot into Oxygen Recovery to flash factory images
	iEcho("Temporarily booting Oxygen recovery to flash latest OxygenOS...")
	err = fastboot.Boot(oxygenrecovery)
	if err != nil {
		eEcho("Failed to boot into Oxygen Recovery: " + err.Error())
		exit(ErrorTWRP)
	}

	// Wait for user to select install form usb option
	fmt.Print("On OnePlus5, choose Install from USB option in the recovery screen, tap OK to confirm. Press enter when in sideload mode")
	responseBytes, _, err := reader.ReadLine()
	if err != nil {
		iEcho("Failed to read input: ", err.Error())
		exit(ErrorUserInput)
	}

	err = adb.Sideload(factory)
	if err != nil {
		eEcho("Failed to flash Factory zip file: " + err.Error())
		exit(ErrorTWRP)
	}

	// Wait for user to select install form usb option
	fmt.Print("Reboot back into fastboot when completed.  Press return when in fastboot mode")
	responseBytes, _, err := reader.ReadLine()
	if err != nil {
		iEcho("Failed to read input: ", err.Error())
		exit(ErrorUserInput)
	}

	iEcho("Temporarily booting TWRP to flash Nethunter update zip...")
	err = fastboot.Boot(twrp)
	if err != nil {
		eEcho("Failed to boot TWRP: " + err.Error())
		exit(ErrorTWRP)
	}

	time.Sleep(10000 * time.Millisecond) // 10 seconds

	iEcho("Transferring the Nethunter update zip to your device...")
	if err = adb.PushFg(zip, "/sdcard"); err != nil {
		eEcho("Failed to push Nethunter update zip to device: " + err.Error())
		exit(ErrorAdb)
	}

	iEcho("Installing Nethunter, please keep your device connected...")
	err = adb.Shell("twrp install /sdcard/" + zip)
	if err != nil {
		eEcho("Failed to flash Nethunter update zip: " + err.Error())
		exit(ErrorTWRP)
	}

	// Pause a bit after install or TWRP gets confused
	time.Sleep(2000 * time.Millisecond)

	iEcho("Wiping your device without wiping /data/media...")
	err = adb.Shell("twrp wipe cache")
	if err != nil {
		eEcho("Failed to wipe cache: " + err.Error())
		exit(ErrorTWRP)
	}
	time.Sleep(1000 * time.Millisecond)
	err = adb.Shell("twrp wipe dalvik")
	if err != nil {
		eEcho("Failed to wipe dalvik: " + err.Error())
		exit(ErrorTWRP)
	}
	time.Sleep(1000 * time.Millisecond)
	err = adb.Shell("twrp wipe data")
	if err != nil {
		eEcho("Failed to wipe data: " + err.Error())
		exit(ErrorTWRP)
	}
	time.Sleep(1000 * time.Millisecond)

	iEcho(MsgSuccess)
	err = adb.Reboot("")
	if err != nil {
		eEcho("Failed to reboot: " + err.Error())
		iEcho("\nPlease reboot your device manually by going to Reboot > System > Do Not Install")
		exit(ErrorAdb)
	}

	exit(Success)
}
