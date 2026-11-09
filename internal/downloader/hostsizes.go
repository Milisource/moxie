// Package downloader provides HTTP download functionality with resume support
// and progress reporting for game files. It includes host-specific resolvers
// for the F95Zone approved file hosts list.
package downloader

// HostSizeLimit returns the maximum file size in bytes allowed by a given host.
// Returns 0 if the host is unlimited or unknown.
func HostSizeLimit(host string) int64 {
	switch host {
	case "vern":
		return 256 * 1024 * 1024 // 256 MB
	case "1cloudfile":
		return 10 * 1024 * 1024 * 1024 // 10 GB
	case "akirabox":
		return 5 * 1024 * 1024 * 1024 // 5 GB (Probationary)
	case "anontransfer":
		return 50 * 1024 * 1024 * 1024 // 50 GB
	case "anonymfile":
		return 5 * 1024 * 1024 * 1024 // 5 GB
	case "apkadmin":
		return 2 * 1024 * 1024 * 1024 // 2 GB
	case "bowfile":
		return 20 * 1024 * 1024 * 1024 // 20 GB
	case "bunkrr":
		return 2 * 1024 * 1024 * 1024 // 2 GB
	case "buzzheavier":
		return 0 // Unlimited
	case "catbox":
		return 200 * 1024 * 1024 // 200 MB
	case "cyberfile":
		return 10 * 1024 * 1024 * 1024 // 10 GB
	case "datanodes":
		return 250 * 1024 * 1024 * 1024 // 250 GB
	case "delafil":
		return 6 * 1024 * 1024 * 1024 // 6 GB
	case "downloadgg":
		return 25 * 1024 * 1024 * 1024 // 25 GB
	case "dropmefiles":
		return 50 * 1024 * 1024 * 1024 // 50 GB
	case "easyupload":
		return 10 * 1024 * 1024 * 1024 // 10 GB
	case "filemail":
		return 5 * 1024 * 1024 * 1024 // 5 GB
	case "filesdpua":
		return 100 * 1024 * 1024 * 1024 // 100 GB
	case "filesfm":
		return 5 * 1024 * 1024 * 1024 // 5 GB
	case "fromsmash":
		return 2 * 1024 * 1024 * 1024 // 2 GB
	case "gofile":
		return 30 * 1024 * 1024 * 1024 // 30 GB
	case "googledrive":
		return 15 * 1024 * 1024 * 1024 // 15 GB
	case "hexload":
		return 2 * 1024 * 1024 * 1024 // 2 GB
	case "krakenfiles":
		return 1 * 1024 * 1024 * 1024 // 1 GB
	case "mediafire":
		return 10 * 1024 * 1024 * 1024 // 10 GB
	case "mega":
		return 15 * 1024 * 1024 * 1024 // 15 GB
	case "mixdrop":
		return 10 * 1024 * 1024 * 1024 // 10 GB
	case "pixeldrain":
		return 10 * 1024 * 1024 * 1024 // 10 GB
	case "protondrive":
		return 1 * 1024 * 1024 * 1024 // 1 GB
	case "quax":
		return 256 * 1024 * 1024 // 256 MB
	case "sendgb":
		return 5 * 1024 * 1024 * 1024 // 5 GB
	case "terminal":
		return 0 // Invite only, unknown limit
	case "transfersh":
		return 0 // Unlimited
	case "transfert":
		return 10 * 1024 * 1024 * 1024 // 10 GB
	case "uploadhaven":
		return 0 // Invite only, unknown limit
	case "uploadnow":
		return 100 * 1024 * 1024 * 1024 // 100 GB
	case "vikingfile":
		return 0 // Unlimited
	case "wdho":
		return 3 * 1024 * 1024 * 1024 // 3 GB
	case "wetransfer":
		return 2 * 1024 * 1024 * 1024 // 2 GB
	case "workupload":
		return 2 * 1024 * 1024 * 1024 // 2 GB
	case "yourfilestore":
		return 500 * 1024 * 1024 // 500 MB
	default:
		return 0 // Unknown limit
	}
}

// list of most commonly used hosts (for priority sorting in selectBestLinkByPlatform)
var PopularHosts = []string{
	"vikingfile",
	"buzzheavier",
	"pixeldrain",
	"mega",
	"gofile",
	"mediafire",
	"workupload",
	"krakenfiles",
}
