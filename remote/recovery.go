package remote

import "github.com/pdsouza/toolbox.go/net"

func RequestOxygenRecovery() (req *net.DownloadRequest, err error) {
	url := 'http://oxygenos.oneplus.net.s3.amazonaws.com/OP5_recovery.img'

	req, err = net.NewDownloadRequest(url)
	if err != nil {
		return nil, err
	}

	return req, nil
}