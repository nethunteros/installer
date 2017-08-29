package remote

import "github.com/pdsouza/toolbox.go/net"

func RequestFactory() (req *net.DownloadRequest, err error) {
	url := "http://oxygenos.oneplus.net.s3.amazonaws.com/OnePlus5Oxygen_23_OTA_013_all_1708032241_1213265a0ad04ecf.zip"

	req, err = net.NewDownloadRequest(url)
	if err != nil {
		return nil, err
	}

	return req, nil
}