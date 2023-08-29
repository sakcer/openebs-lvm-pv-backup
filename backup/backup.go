package backup

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"

	api "restic/api/v1"
)

func (b *BackupOperator) ResticBackup(repo, filepath, backupName, pswd string, tags api.Tags) error {
	str := fmt.Sprintf("%s:.", filepath)
	resticCmd := exec.Command("proot", "-b", str, "restic", "backup", ".", "--tag", tags.BackupName, "--tag", tags.Namespace, "--tag", tags.Pvc)

	if out, err := resticCmd.CombinedOutput(); err != nil {
		fmt.Println("backup failed")
		return fmt.Errorf(err.Error(), string(out))
	}
	fmt.Println("backup completed")
	return nil
}

func (b *BackupOperator) DirEmpty(path string) (bool, error) {
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

func (b *BackupOperator) Mount(src, des string) error {
	if out, err := exec.Command("mount", src, des).CombinedOutput(); err != nil {
		// fmt.Printf("start mount failed (%s, %s, %s, %s)\n", err, string(out), src, des)
		return fmt.Errorf("start mount failed (%s)", string(out))
	}
	fmt.Printf("start mount completed (%s %s)\n", src, des)

	return nil
}

func (b *BackupOperator) Unmount(des string) error {
	if out, err := exec.Command("umount", des).CombinedOutput(); err != nil {
		// fmt.Printf("unmount failed (%s, %s)\n", des, string(out))
		return fmt.Errorf("unmount failed (%s)\n", string(out))
	}

	fmt.Printf("unmount completed (%s)\n", des)
	return nil
}

func (b *BackupOperator) Backup(pv string, tags api.Tags) error {
	src := path.Join(api.Source, pv)

	snapshots, err := b.ResticSnapshots(tags)
	if err != nil {
		return err
	} else if len(snapshots) != 0 {
		return errors.New("backup already exists")
	}

	tmpDir, err := ioutil.TempDir("", "pvc-backup")
	if err != nil {
		return err
	}

	if err := b.Mount(src, tmpDir); err != nil {
		return err
	}

	if err := b.ResticBackup(api.Repo, tmpDir, pv, api.Password, tags); err != nil {
		return err
	}

	if err := b.Unmount(tmpDir); err != nil {
		return err
	}

	if err := os.RemoveAll(tmpDir); err != nil {
		return err
	}

	return nil
}
