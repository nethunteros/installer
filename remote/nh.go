package remote

import "github.com/pdsouza/toolbox.go/net"

func RequestNethunter() (req *net.DownloadRequest, err error) {
	
	url := "https://build.nethunter.com/misc/nethunter-oneplus5-oos-nougat-kalifs-full-20170828_192201.zip"

	req, err = net.NewDownloadRequest(url)
	
	if err != nil {
		return nil, err
	}

	return req, nil
}