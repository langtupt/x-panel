package quota

import (
	"strings"
	"testing"
)

func TestHomeMount(t *testing.T) {
	cases := []struct {
		name, mounts, mount string
		on                  bool
	}{
		{"root only, no quota", "/dev/sda1 / ext4 rw,relatime 0 0\n", "/", false},
		{"root with usrquota", "/dev/sda1 / ext4 rw,relatime,quota,usrquota 0 0\n", "/", true},
		{"separate /home xfs", "/dev/sda1 / xfs rw 0 0\n/dev/sdb1 /home xfs rw,uquota 0 0\n", "/home", true},
		{"ignore /homes", "/dev/sda1 / ext4 rw,usrquota 0 0\n/dev/sdc1 /homes ext4 rw 0 0\n", "/", true},
		{"journaled quota", "/dev/sda1 / ext4 rw,usrjquota=aquota.user,jqfmt=vfsv1 0 0\n", "/", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, on := homeMount(strings.NewReader(c.mounts))
			if m != c.mount || on != c.on {
				t.Fatalf("got (%s,%v), want (%s,%v)", m, on, c.mount, c.on)
			}
		})
	}
}

func TestParseCSV(t *testing.T) {
	out := "User,BlockStatus,FileStatus,BlockUsed,BlockSoftLimit,BlockHardLimit,BlockGrace,FileUsed,FileSoftLimit,FileHardLimit,FileGrace\n" +
		"root,ok,ok,13,0,0,,2,0,0,\n" +
		"u1,ok,ok,50183,51200,51200,,6,1000,1000,\n"
	got, err := parseCSV(out)
	if err != nil {
		t.Fatal(err)
	}
	if u := got["u1"]; u.DiskUsedMB != 50 || u.InodesUsed != 6 {
		t.Fatalf("u1 = %+v", u)
	}
}
