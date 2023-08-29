package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	api "restic/api/v1"
)

func (b *BackupOperator) ResticSnapshots(tags api.Tags) ([]api.SnapShot, error) {
	out, err := exec.Command("restic", "snapshots", "--tag", tags.Namespace, "--tag", tags.BackupName, "--json", "--quiet").Output()
	if err != nil {
		return []api.SnapShot{}, err
	}

	snapshots := []api.SnapShot{}
	if err := json.Unmarshal(out, &snapshots); err != nil {
		return []api.SnapShot{}, err
	}

	// if len(snapshots) != 1 {
	// 	return api.SnapShot{}, errors.New("somethin wrong happened")
	// }

	return snapshots, nil
}

func (b *BackupOperator) ResticRestore(des string, id string) error {
	resticCmd := exec.Command("restic", "restore", "--target", des, id)
	if out, err := resticCmd.CombinedOutput(); err != nil {
		return fmt.Errorf(string(out))
	}
	fmt.Println("restore completed")
	return nil
}

func (b *BackupOperator) Restore(pv string, tags api.Tags) error {
	// des := path.Join(api.TmpDir, pv)
	src := path.Join(api.Source, pv)

	snapshots, err := b.ResticSnapshots(tags)
	if err != nil {
		return err
	} else if len(snapshots) != 1 {
		return errors.New("something wrong happend")
	}

	tmpDir, err := ioutil.TempDir("", "pvc-backup")
	if err != nil {
		return err
	}

	if err := b.Mount(src, tmpDir); err != nil {
		return err
	}
	if err := b.ResticRestore(tmpDir, snapshots[0].ID); err != nil {
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
