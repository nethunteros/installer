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
	"os"
	"path"
	"runtime"
	"time"

	"./android"
	"./remote"

	"github.com/pdsouza/toolbox.go/ui"
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
	productName, err := fastboot.GetProduct()

	if err != nil {
		eEcho("Failed to get device product info: " + err.Error())
		exit(ErrorFastboot)
	}

	// OnePlus references there phones as below
	if "QC_Reference_Phone" == productName {
		iEcho("OnePlus 5!")
	} else {
		eEcho("This is probably not a OnePlus5....going to continue anyways!? YOLO")
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

	// Request nethunter
	nhzip := "nethunter-oneplus5-oos-nougat-kalifs-full-20170828_192201.zip"
	if _, err := os.Stat(nhzip); os.IsNotExist(err) { // If file missing, download
		nhzipurl := "https://build.nethunter.com/misc/nethunter-oneplus5-oos-nougat-kalifs-full-20170828_192201.zip"
		remote.DownloadURL(nhzipurl)
	}

	// Download Recovery Image
	oxygenrecovery := "OP5_recovery.img"
	if _, err := os.Stat(oxygenrecovery); os.IsNotExist(err) { // If file missing, download
		recoveryimgurl := "http://oxygenos.oneplus.net.s3.amazonaws.com/OP5_recovery.img"
		remote.DownloadURL(recoveryimgurl)
	}

	// Download Factory Image
	factory := "OnePlus5Oxygen_23_OTA_013_all_1708032241_1213265a0ad04ecf.zip"
	if _, err := os.Stat(factory); os.IsNotExist(err) { // If file missing, download
		oxygenurl := "http://oxygenos.oneplus.net.s3.amazonaws.com/OnePlus5Oxygen_23_OTA_013_all_1708032241_1213265a0ad04ecf.zip"
		remote.DownloadURL(oxygenurl)
	}

	// Download TWRP
	twrp := "twrp-3.1.1-1-cheeseburger.img"
	if _, err := os.Stat(factory); os.IsNotExist(err) { // If file missing, download
		twrpurl := "https://dl.twrp.me/cheeseburger/twrp-3.1.1-1-cheeseburger.img"
		remote.DownloadURL(twrpurl)
	}

	// ------------------------ START INSTALL ------------------ //

	// Boot into Oxygen Recovery to flash factory images
	iEcho("Flashing OxygenOS recovery in order to flash latest OxygenOS...")
	// If you don't flash factory recovery first it will try to reboot into TWRP and fail afterwards
	err = fastboot.FlashRecovery(oxygenrecovery)
	if err != nil {
		eEcho("Failed to flash Oxygen Recovery: " + err.Error())
		exit(ErrorTWRP)
	}

	// Boot into the recovery image
	iEcho("Booting into OxygenOS Recovery")
	err = fastboot.Boot(oxygenrecovery)
	if err != nil {
		eEcho("Failed to boot into Oxygen Recovery: " + err.Error())
		exit(ErrorTWRP)
	}

	// Wait for user to select install form usb option in recovery
	fmt.Printf("On OnePlus5, select language using volume and power button.\nChoose Install from ADB option in the recovery screen, tap yes to continue.\nPress enter when in sideload mode")
	bufio.NewReader(os.Stdin).ReadBytes('\n')

	fmt.Print("Flashing factory zip file.  This can take ~10 minutes.")

	err = adb.Sideload(factory)
	if err != nil {
		eEcho("Failed to flash Factory zip file: " + err.Error())
		exit(ErrorTWRP)
	}

	// Wait for user to select install form usb option
	fmt.Printf("Reboot.  Go through steps of enabling ADB again.  Accept RSA key.  Press enter when ready")
	bufio.NewReader(os.Stdin).ReadBytes('\n')

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

	// Flash TWRP recovery (no longer need oxygen recovery)
	err = fastboot.FlashRecovery(twrp)
	if err != nil {
		eEcho("Failed to flash Oxygen Recovery: " + err.Error())
		exit(ErrorTWRP)
	}

	iEcho("Temporarily booting TWRP to flash Nethunter update zip (allow system modification)...")
	err = fastboot.Boot(twrp)
	if err != nil {
		eEcho("Failed to boot TWRP: " + err.Error())
		exit(ErrorTWRP)
	}

	time.Sleep(30000 * time.Millisecond) // 30 seconds

	iEcho("Transferring the Nethunter update zip to your device...")
	if err = adb.PushFg(nhzip, "/sdcard"); err != nil {
		eEcho("Failed to push Nethunter update zip to device: " + err.Error())
		exit(ErrorAdb)
	}

	iEcho("Installing Nethunter, please keep your device connected...")
	err = adb.Shell("twrp install /sdcard/" + nhzip)
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
