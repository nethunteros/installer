//
// Copyright 2017 The Maru OS Project
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
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
	"log"
	"os"
	"path"
	"runtime"
	"time"

	"./android"
	"./remote"

	"github.com/dixonwille/wmenu"
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

func waitForOpKey(msg string) {
	fmt.Printf(msg)
	bufio.NewReader(os.Stdin).ReadBytes('\n')
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
	nhDevices := readDevicesConfig()

	/*
		Step 1 - Set path to binaries
		Step 2 - Verify ADB and Fastboot
		Step 3 - Check USB permissions
		Step 4 - Identify this is the correct device
		Step 5 - Detect if device is unlocked, then unlock
		Step 6 - Download: NethunterOS, TWRP Recovery, Kali Filesystem
		Step 7 - Boot into TWRP
		Step 8 - Install NH

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
	// (We can remove this later)
	eEcho("The installer supports the following devices: ")
	for _, d := range nhDevices.Device {
		fmt.Printf("    - %s (%s)\n", d.Common_name, d.Product_name)
	}
	fmt.Print("\nAre you ready to install Nethunter? (yes/no): ")
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

	// OnePlus uses the same board name for every device.  Need to let user select
	if productName == "QC_Reference_Phone" {
		menu := wmenu.NewMenu("Detected OnePlus device.  Select which device: ")
		menu.Action(func(opts []wmenu.Opt) error { productName = opts[0].Text; return nil })
		menu.Option("OnePlus 5", nil, true, nil)
		menu.Option("OnePlus 2", nil, false, nil)
		menu.Option("OnePlus 1", nil, false, nil)
		err := menu.Run()
		if err != nil {
			log.Fatal(err)
		}
	}

	if err != nil {
		eEcho("Failed to get device product info: " + err.Error())
		exit(ErrorFastboot)
	}
	currDevice := findDeviceConfig(nhDevices, productName)

	// Check that we have the device config in the file

	if currDevice.Common_name != "" {
		fmt.Printf("Device and config found, using %s (%s) configuration and endpoints\n", currDevice.Common_name, currDevice.Product_name)
	} else {
		eEcho("Device config not found! Bye.")
		exit(1)
	}

	waitForOpKey("Press enter to continue with bootloader unlock check. Unlocking will wipe device if first time and will require restart.") // not sure about the sentence here

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

	// Check if there is any other extra files we need to get
	if currDevice.Extra_file != "" && currDevice.Extra_url != "" {
		if _, err := os.Stat(currDevice.Extra_file); os.IsNotExist(err) { // If file missing, download
			remote.DownloadURL(currDevice.Extra_url)
		}
	}

	// Request nethunter OS
	if _, err := os.Stat(currDevice.Nhos_file); os.IsNotExist(err) { // If file missing, download
		remote.DownloadURL(currDevice.Nhos_url)
	}

	// Request nethunter generic fileysstem
	if _, err := os.Stat(currDevice.Nhfs_file); os.IsNotExist(err) { // If file missing, download
		remote.DownloadURL(currDevice.Nhfs_url)
	}

	// Request gapps
	if _, err := os.Stat(currDevice.Gapps_file); os.IsNotExist(err) { // If file missing, download
		remote.DownloadURL(currDevice.Gapps_url)
	}

	// Download TWRP
	if _, err := os.Stat(currDevice.Twrp_file); os.IsNotExist(err) { // If file missing, download
		remote.DownloadURL(currDevice.Twrp_url)
	}

	waitForOpKey("Press enter to start the installation")

	// Flash TWRP recovery
	iEcho("Starting TWRP flash")
	err = fastboot.FlashRecovery(currDevice.Twrp_file)
	if err != nil {
		eEcho("Failed to flash TWRP Recovery: " + err.Error())
		exit(ErrorTWRP)
	}

	// Boot into twrp
	iEcho("Booting TWRP to flash Nethunter update zip.\n Swipe to allow system modification in TWRP and wait")
	err = fastboot.Boot(currDevice.Twrp_file)
	if err != nil {
		eEcho("Failed to boot TWRP: " + err.Error())
		exit(ErrorTWRP)
	}

	// Wait for TWRP
	waitForOpKey("Press enter when TWRP is fully loaded & ready")

	// Start fresh
	iEcho("Removing previous installations")
	time.Sleep(1000 * time.Millisecond)
	err = adb.Shell("twrp wipe dalvik")
	if err != nil {
		eEcho("Failed to wipe dalvik: " + err.Error())
		exit(ErrorTWRP)
	}

	iEcho("Removing previous /data")
	time.Sleep(1000 * time.Millisecond)
	err = adb.Shell("twrp wipe data")
	if err != nil {
		eEcho("Failed to wipe data: " + err.Error())
		exit(ErrorTWRP)
	}

	iEcho("Removing previous /system")
	time.Sleep(1000 * time.Millisecond)
	err = adb.Shell("twrp wipe system")
	if err != nil {
		eEcho("Failed to wipe system: " + err.Error())
		exit(ErrorTWRP)
	}

	// Transfer any extra files we need to flash
	if currDevice.Extra_file != "" {
		iEcho("Transferring extra zip (firmware/etc) to your device...")
		if err = adb.PushFg(currDevice.Extra_file, "/sdcard"); err != nil {
			eEcho("Failed to push extra update zip to device: " + err.Error())
			exit(ErrorAdb)
		}
	}

	// Transfer ROM to sdcard then install in TWRP
	iEcho("Transferring the NethunterOS zip to your device...")
	if err = adb.PushFg(currDevice.Nhos_file, "/sdcard"); err != nil {
		eEcho("Failed to push NethunterOS update zip to device: " + err.Error())
		exit(ErrorAdb)
	}

	// Transfer filesystem with app to sdcard then install
	iEcho("Transferring the Nethunter filesystem zip to your device...")
	if err = adb.PushFg(currDevice.Nhfs_file, "/sdcard"); err != nil {
		eEcho("Failed to push Nethunter update zip to device: " + err.Error())
		exit(ErrorAdb)
	}

	// Transfer filesystem with app to sdcard then install
	iEcho("Transferring the Google Apps zip to your device...")
	if err = adb.PushFg(currDevice.Gapps_file, "/sdcard"); err != nil {
		eEcho("Failed to push Google Apps zip to device: " + err.Error())
		exit(ErrorAdb)
	}

	// Extras should be installed first (like Device firmware or baseband)
	// Otherwise NHOS will fail
	if currDevice.Extra_file != "" {
		iEcho("Installing extra zip (firmware/baseband/etc) please keep your device connected...")
		err = adb.Shell("twrp install /sdcard/" + currDevice.Extra_file)
		if err != nil {
			eEcho("Failed to flash extra update zip: " + err.Error())
			exit(ErrorTWRP)
		}
	}

	// Start installer for ROM, Gapps, then Nethunter chroot & apps
	iEcho("Installing NethunterOS please keep your device connected...")
	err = adb.Shell("twrp install /sdcard/" + currDevice.Nhos_file)
	if err != nil {
		eEcho("Failed to flash Nethunter update zip: " + err.Error())
		exit(ErrorTWRP)
	}

	// Install gapps?
	actFunc := func(opts []wmenu.Opt) error {
		if opts[0].ID == 0 {
			iEcho("Installing Gapps...")
			err = adb.Shell("twrp install /sdcard/" + currDevice.Gapps_file)
			if err != nil {
				eEcho("Failed to flash Google Apps: " + err.Error())
				exit(ErrorTWRP)
			}
		}
		if opts[0].ID == 1 {
			fmt.Println("Skipping Gapps install")
		}
		return nil
	}

	menu := wmenu.NewMenu("Install Gapps?") // The yes or no question
	menu.Action(actFunc)
	menu.IsYesNo(0)
	err = menu.Run()
	if err != nil {
		log.Fatal(err)
	}

	// Pause a bit after install or TWRP gets confused
	// is this allways enought?
	time.Sleep(10000 * time.Millisecond)
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

	iEcho(MsgSuccess)
	err = adb.Reboot("")
	if err != nil {
		eEcho("Failed to reboot: " + err.Error())
		iEcho("\nPlease reboot your device manually by going to Reboot > System > Do Not Install")
		exit(ErrorAdb)
	}
	// Wait for user to select install form usb option
	iEcho(MsgReenable)
	waitForOpKey("Press enter when ADB is reenabled")

	verifyAdbStatusOrAbort(&adb)

	iEcho("Rebooting your device into bootloader...")
	err = adb.Reboot("bootloader")
	if err != nil {
		eEcho("Failed to reboot into bootloader: " + err.Error())
		exit(ErrorAdb)
	}

	time.Sleep(30000 * time.Millisecond) // 30 seconds // maybe add waitForOpKey here also?

	// Boot into twrp
	iEcho("Booting TWRP to flash Nethunter update zip.\n Swipe to allow system modification in TWRP and wait")
	err = fastboot.Boot(currDevice.Twrp_file)
	if err != nil {
		eEcho("Failed to boot TWRP: " + err.Error())
		exit(ErrorTWRP)
	}

	// Wait for TWRP
	waitForOpKey("Press enter when TWRP is fully loaded & ready")

	time.Sleep(20000 * time.Millisecond) // maybe add waitForOpKey here also?
	iEcho("Installing Nethunter filesystem, please keep your device connected...")
	err = adb.Shell("twrp install /sdcard/" + currDevice.Nhfs_file)
	if err != nil {
		eEcho("Failed to flash Nethunter update zip: " + err.Error())
		exit(ErrorTWRP)
	}

	time.Sleep(30000 * time.Millisecond) // 30 seconds // maybe add waitForOpKey here also?

	iEcho(MsgSuccess)
	err = adb.Reboot("")
	if err != nil {
		eEcho("Failed to reboot: " + err.Error())
		iEcho("\nPlease reboot your device manually by going to Reboot > System > Do Not Install")
		exit(ErrorAdb)
	}

	exit(Success)
}
