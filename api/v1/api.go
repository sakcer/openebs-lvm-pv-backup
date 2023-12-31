package v1

import "os"

type Tags struct {
	Namespace  string
	BackupName string
	Pvc        string
}

type SnapShot struct {
	ID      string `json:"id"`
	ShortID string `json:"short_id"`
	Time    string `json:"time"`
}

type SnapShotList struct {
	SnapShots []SnapShot
}

var (
	Source          = "/dev/lvmvg"
	TmpDir          = "/tmp"
	Repo            = "s3:http://localhost:9000/test"
	Password        = "test"
	Endpoint        = "10.162.17.233:9000" // MinIO server endpoint
	AccessKeyID     = "test"               // MinIO access key
	SecretAccessKey = "testtest"           // MinIO secret key
)

func init() {
	Password = os.Getenv("PASSWORD")
	AccessKeyID = os.Getenv("AWS_ACCESS_KEY_ID")
	SecretAccessKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	Repo = os.Getenv("RESTIC_REPOSITORY")
	Password = os.Getenv("RESTIC_PASSWORD")
}
