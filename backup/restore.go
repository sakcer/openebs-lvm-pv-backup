package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	api "restic/api/v1"
)

func (b *BackupOperator) ResticSnapshots(tags api.Tags) ([]api.SnapShot, error) {
	out, err := exec.Command("restic", "snapshots", "--tag", fmt.Sprintf("%s,%s", tags.Namespace, tags.BackupName), "--json", "--quiet").Output()
	if err != nil {
		return []api.SnapShot{}, err
	}
	fmt.Println(string(out))

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
	// if err := resticCmd.Wait(); err != nil {
	// 	return err
	// }
	fmt.Println("restore completed")
	return nil
}

func (b *BackupOperator) Restore(path string, tags api.Tags) (bool, error) {
	// des := path.Join(api.TmpDir, pv)
	// src := path.Join(api.Source, pv)

	snapshots, err := b.ResticSnapshots(tags)
	if err != nil {
		return false, err
	} else if len(snapshots) != 1 {
		return true, errors.New("something wrong happend")
	}

	// tmpDir := fmt.Sprintf("/tmp/restore-%s", uuid)

	// if _, err := os.Stat(tmpDir); !os.IsNotExist(err) {
	// 	b.Unmount(tmpDir)
	// 	if err := os.Remove(tmpDir); err != nil {
	// 		return false, err
	// 	}
	// }
	// if out, err := exec.Command("mkdir", tmpDir).CombinedOutput(); err != nil {
	// 	return false, fmt.Errorf("Mkdir %s", string(out))
	// }
	// tmpDir, err := ioutil.TempDir("", "pvc-backup")
	// if err != nil {
	// 	return err
	// }

	// if err := b.Mount(src, tmpDir); err != nil {
	// 	return false, err
	// }
	if err := b.ResticRestore(path, snapshots[0].ID); err != nil {
		return false, err
	}
	// if err := b.Unmount(tmpDir); err != nil {
	// 	return false, err
	// }

	// if err := os.RemoveAll(tmpDir); err != nil {
	// 	return false, err
	// }

	return true, nil
}
