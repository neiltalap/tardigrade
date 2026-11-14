// Copyright 2017 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package server_test

import (
	"context"
	gosql "database/sql"
	"fmt"
	"testing"

	"github.com/podomy/tardigrade/pkg/base"
	"github.com/podomy/tardigrade/pkg/settings/cluster"
	"github.com/podomy/tardigrade/pkg/testutils"
	"github.com/podomy/tardigrade/pkg/testutils/testcluster"
	"github.com/podomy/tardigrade/pkg/util/leaktest"
	"github.com/podomy/tardigrade/pkg/util/protoutil"
)

type testClusterWithHelpers struct {
	*testing.T
	*testcluster.TestCluster
	args func() map[int]base.TestServerArgs
}

func (th *testClusterWithHelpers) getVersionFromShow(i int) string {
	var version string
	if err := th.ServerConn(i).QueryRow("SHOW CLUSTER SETTING version").Scan(&version); err != nil {
		th.Fatalf("%d: %s", i, err)
	}
	return version
}

func (th *testClusterWithHelpers) getVersionFromSelect(i int) string {
	var version string
	if err := th.ServerConn(i).QueryRow("SELECT value FROM system.settings WHERE name = 'version'").Scan(&version); err != nil {
		if err == gosql.ErrNoRows {
			return ""
		}
		th.Fatalf("%d: %s (%T)", i, err, err)
	}
	var v cluster.ClusterVersion
	if err := protoutil.Unmarshal([]byte(version), &v); err != nil {
		th.Fatalf("%d: %s", i, err)
	}
	return v.Version.String()
}

func (th *testClusterWithHelpers) getVersionFromSetting(i int) *cluster.ExposedClusterVersion {
	return &th.Servers[i].ClusterSettings().Version
}

func (th *testClusterWithHelpers) setVersion(i int, version string) error {
	_, err := th.ServerConn(i).Exec("SET CLUSTER SETTING version = $1", version)
	return err
}

func (th *testClusterWithHelpers) mustSetVersion(i int, version string) {
	if err := th.setVersion(i, version); err != nil {
		th.Fatalf("%d: %s", i, err)
	}
}

func (th *testClusterWithHelpers) setDowngrade(i int, version string) error {
	_, err := th.ServerConn(i).Exec("SET CLUSTER SETTING cluster.preserve_downgrade_option = $1", version)
	return err
}

func (th *testClusterWithHelpers) resetDowngrade(i int) error {
	_, err := th.ServerConn(i).Exec("RESET CLUSTER SETTING cluster.preserve_downgrade_option")
	return err
}

// Test that, after cluster bootstrap, the different ways of getting the cluster
// version all agree.
func TestAllVersionsAgree(t *testing.T) {
	defer leaktest.AfterTest(t)()
	ctx := context.Background()

	tcRaw := testcluster.StartTestCluster(t, 3, base.TestClusterArgs{})
	defer tcRaw.Stopper().Stop(ctx)
	tc := testClusterWithHelpers{
		T:           t,
		TestCluster: tcRaw,
	}

	exp := cluster.BinaryServerVersion.String()

	// The node bootstrapping the cluster starts at BinaryServerVersion, the
	// others start at MinimumSupportedVersion and it takes them a gossip update
	// to get to BinaryServerVersion. Hence, we loop until that gossip comes.
	testutils.SucceedsSoon(tc, func() error {
		for i := 0; i < tc.NumServers(); i++ {
			if version := tc.getVersionFromSetting(i).Version().Version.String(); version != exp {
				return fmt.Errorf("%d: incorrect version %s (wanted %s)", i, version, exp)
			}
			if version := tc.getVersionFromShow(i); version != exp {
				return fmt.Errorf("%d: incorrect version %s (wanted %s)", i, version, exp)
			}
			if version := tc.getVersionFromSelect(i); version != exp {
				return fmt.Errorf("%d: incorrect version %q (wanted %s)", i, version, exp)
			}
		}
		return nil
	})
}
