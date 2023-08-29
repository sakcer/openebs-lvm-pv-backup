package backup

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"

	api "restic/api/v1"
)

// func ResticAddTag(backupName string, tags api.Tags) error {
// 	resticCmd := exec.Command("restic", "tag", "--set", tags.BackupName, "--set", tags.Namespace)
// 	if err := resticCmd.Run(); err != nil {
// 		fmt.Println("add tags failed")
// 		return err
// 	}
// 	fmt.Println("add tags succeed")
// 	return nil
// }

func ResticBackup(repo, filepath, backupName, pswd string, tags api.Tags) error {
	env := append(os.Environ(), fmt.Sprintf("CHROOT=%s", "/root/mount/test/tmp/cmd"))

	resticCmd := exec.Command("restic", "backup", ".", "--tag", tags.BackupName, "--tag", tags.Namespace)
	resticCmd.Env = env
	if out, err := resticCmd.CombinedOutput(); err != nil {
		fmt.Println("backup failed")
		return fmt.Errorf(err.Error(), string(out))
	}
	fmt.Println("backup completed")
	return nil
}

func DirEmpty(path string) (bool, error) {
	dir, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer dir.Close()
	if _, err := dir.Readdir(1); err == io.EOF {
		return true, nil
	} else {
		return false, err
	}
}

func Mount(src, des string) error {
	if err := os.Mkdir(des, os.ModePerm); err != nil {
		if !os.IsExist(err) {
			return fmt.Errorf("(%s)", err)
		}
	}

	if out, err := exec.Command("mount", src, des).CombinedOutput(); err != nil {
		fmt.Printf("start mount failed (%s, %s, %s, %s)\n", err, string(out), src, des)
		return err
	}
	fmt.Printf("start mount completed (%s %s)\n", src, des)

	return nil
}

func Unmount(des string) error {
	if out, err := exec.Command("umount", des).CombinedOutput(); err != nil {
		fmt.Printf("unmount failed (%s, %s)\n", des, string(out))
		return err
	}

	if err := os.Remove(des); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	}
	fmt.Printf("unmount completed (%s)\n", des)
	return nil
}

func Backup(pv string, tags api.Tags) error {
	des := path.Join(api.TmpDir, pv)
	src := path.Join(api.Source, pv)

	snapshots, err := ResticSnapshots(tags)
	if err != nil {
		return err
	} else if len(snapshots) != 0 {
		return errors.New("backup already exists")
	}

	if empty, err := DirEmpty(des); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	} else if !empty {
		if err := Unmount(des); err != nil {
			return err
		}
	}

	if err := Mount(src, des); err != nil {
		return err
	}

	if err := ResticBackup(api.Repo, des, pv, api.Password, tags); err != nil {
		return err
	}

	if err := Unmount(des); err != nil {
		return err
	}

	return nil
}
