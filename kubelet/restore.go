package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	api "restic/api/v1"
)

func ResticSnapshots(tags api.Tags) ([]api.SnapShot, error) {
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

func ResticRestore(des string, id string) error {
	resticCmd := exec.Command("restic", "restore", "--target", des, id)
	if out, err := resticCmd.CombinedOutput(); err != nil {
		return fmt.Errorf(string(out))
	}
	return nil
}

func Restore(pv string, tags api.Tags) error {
	des := path.Join(api.TmpDir, pv)
	src := path.Join(api.Source, pv)

	snapshots, err := ResticSnapshots(tags)
	if err != nil {
		return err
	} else if len(snapshots) != 1 {
		return errors.New("something wrong happend")
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
	if err := ResticRestore(des, snapshots[0].ID); err != nil {
		return err
	}
	if err := Unmount(des); err != nil {
		return err
	}
	return nil
}
