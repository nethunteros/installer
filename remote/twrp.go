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

// Simple TWRP release client.

package remote

import "github.com/pdsouza/toolbox.go/net"

const (
	TWRPEndpoint      = "https://dl.twrp.me"
	TWRPVersionPrefix = "twrp-3.1.1-1-"
	TWRPExtension     = ".img"
)

func genTWRPDeviceUrl(device string) string {
	return TWRPEndpoint + "/" + device + "/" + TWRPVersionPrefix + device + TWRPExtension
}

func RequestTWRP(device string) (req *net.DownloadRequest, err error) {
	url := genTWRPDeviceUrl(device)

	req, err = net.NewDownloadRequest(url)
	if err != nil {
		return nil, err
	}

	// TWRP looks for referer header to initiate download
	req.Request.Header.Add("Referer", url)

	return req, nil
}
