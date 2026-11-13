// Copyright 2020 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package server

import (
	"context"
	"fmt"

	"github.com/podomy/tardigrade/pkg/internal/client"
	"github.com/podomy/tardigrade/pkg/keys"
	"github.com/podomy/tardigrade/pkg/roachpb"
	"github.com/podomy/tardigrade/pkg/security"
	"github.com/podomy/tardigrade/pkg/settings/cluster"
	"github.com/podomy/tardigrade/pkg/sql/sem/tree"
	"github.com/podomy/tardigrade/pkg/sql/sessiondata"
	"github.com/podomy/tardigrade/pkg/sql/sqlbase"
	"github.com/podomy/tardigrade/pkg/sql/types"
	"github.com/podomy/tardigrade/pkg/util/log"
	"github.com/podomy/tardigrade/pkg/util/protoutil"
	"github.com/podomy/tardigrade/pkg/util/retry"
	"github.com/pkg/errors"
)

// bootstrapFreshCluster initializes the SQL state required for a new
// cluster. It replaces the old sqlmigrations framework, which is deleted
// under the zero-compatibility premise. It runs only when this node
// bootstrapped the cluster (doBootstrap) and is idempotent.
func (s *Server) bootstrapFreshCluster(ctx context.Context) error {
	if err := s.addJobsProgressColumn(ctx); err != nil {
		return errors.Wrap(err, "adding progress column to system.jobs")
	}
	if err := s.populateVersionSetting(ctx); err != nil {
		return errors.Wrap(err, "populating version setting")
	}
	if err := s.runStmtAsRootWithRetry(
		ctx, "disable-net-trace", `SET CLUSTER SETTING trace.debug.enable = false`,
	); err != nil {
		return err
	}
	if err := s.runStmtAsRootWithRetry(
		ctx, "add-root-user",
		`UPSERT INTO system.users (username, "hashedPassword", "isRole") VALUES ($1, '', false)`,
		security.RootUser,
	); err != nil {
		return err
	}
	if err := s.runStmtAsRootWithRetry(
		ctx, "add-admin-role",
		`UPSERT INTO system.users (username, "hashedPassword", "isRole") VALUES ($1, '', true)`,
		sqlbase.AdminRole,
	); err != nil {
		return err
	}
	if err := s.runStmtAsRootWithRetry(
		ctx, "add-root-to-admin",
		`UPSERT INTO system.role_members ("role", "member", "isAdmin") VALUES ($1, $2, true)`,
		sqlbase.AdminRole, security.RootUser,
	); err != nil {
		return err
	}
	if err := s.runStmtAsRootWithRetry(
		ctx, "init-cluster-secret",
		`SET CLUSTER SETTING cluster.secret = gen_random_uuid()::STRING`,
	); err != nil {
		return err
	}
	for _, dbName := range []string{sessiondata.DefaultDatabaseName, sessiondata.PgDatabaseName} {
		stmt := fmt.Sprintf(`CREATE DATABASE IF NOT EXISTS "%s"`, dbName)
		if err := s.runStmtAsRootWithRetry(ctx, "create-default-db", stmt); err != nil {
			return err
		}
	}
	if err := s.populateLocationData(ctx); err != nil {
		return errors.Wrap(err, "populating system.locations")
	}
	if err := s.retireOldTsSettings(ctx); err != nil {
		return errors.Wrap(err, "retiring old ts settings")
	}
	if err := s.depublicizeSystemComments(ctx); err != nil {
		return errors.Wrap(err, "depublicizing system.comments")
	}
	return nil
}

// runStmtAsRootWithRetry runs a statement as root with bounded retries.
func (s *Server) runStmtAsRootWithRetry(
	ctx context.Context, opName string, stmt string, qargs ...interface{},
) error {
	var err error
	for r := retry.Start(retry.Options{MaxRetries: 5}); r.Next(); {
		_, err = s.internalExecutor.Exec(ctx, opName, nil /* txn */, stmt, qargs...)
		if err == nil {
			break
		}
		log.Warningf(ctx, "failed to run %s: %v", stmt, err)
	}
	return err
}

// addJobsProgressColumn adds the progress column to system.jobs by hand.
// The bootstrap descriptor does not include it; the old migration did this
// via KV. This mirrors that migration exactly.
func (s *Server) addJobsProgressColumn(ctx context.Context) error {
	err := s.db.Txn(ctx, func(ctx context.Context, txn *client.Txn) error {
		err := txn.SetSystemConfigTrigger()
		if err != nil {
			return err
		}
		desc, err := sqlbase.GetMutableTableDescFromID(ctx, txn, keys.JobsTableID)
		if err != nil {
			return err
		}
		if _, err := desc.FindActiveColumnByName("progress"); err == nil {
			return nil
		}
		desc.AddColumn(&sqlbase.ColumnDescriptor{
			Name:     "progress",
			Type:     *types.Bytes,
			Nullable: true,
		})
		err = desc.AddColumnToFamilyMaybeCreate("progress", "progress", true, false)
		if err != nil {
			return err
		}
		err = desc.AllocateIDs()
		if err != nil {
			return err
		}
		return txn.Put(ctx, sqlbase.MakeDescMetadataKey(desc.ID), sqlbase.WrapDescriptor(desc))
	})
	return err
}

// populateVersionSetting writes the initial version row into
// system.settings. This mirrors the old migration exactly.
func (s *Server) populateVersionSetting(ctx context.Context) error {
	var v roachpb.Version
	err := s.db.Txn(ctx, func(ctx context.Context, txn *client.Txn) error {
		return txn.GetProto(ctx, keys.BootstrapVersionKey, &v)
	})
	if err != nil {
		return err
	}
	if v == (roachpb.Version{}) {
		v = cluster.BinaryMinimumSupportedVersion
	}
	b, err := protoutil.Marshal(&cluster.ClusterVersion{Version: v})
	if err != nil {
		return errors.Wrap(err, "while marshaling version")
	}
	_, err = s.internalExecutor.Exec(
		ctx,
		"insert-setting",
		nil, /* txn */
		fmt.Sprintf(`INSERT INTO system.settings (name, value, "lastUpdated", "valueType") VALUES ('version', x'%x', now(), 'm') ON CONFLICT(name) DO NOTHING`, b),
	)
	if err != nil {
		return err
	}
	_, err = s.internalExecutor.Exec(
		ctx, "set-setting", nil /* txn */, "SET CLUSTER SETTING version = $1", v.String(),
	)
	return err
}

// populateLocationData inserts default location rows if the table is empty.
func (s *Server) populateLocationData(ctx context.Context) error {
	row, err := s.internalExecutor.QueryRow(ctx, "update-system-locations",
		nil, `SELECT count(*) FROM system.locations`)
	if err != nil {
		return err
	}
	count := int(tree.MustBeDInt(row[0]))
	if count != 0 {
		return nil
	}
	for _, loc := range roachpb.DefaultLocationInformation {
		stmt := `UPSERT INTO system.locations VALUES ($1, $2, $3, $4)`
		tier := loc.Locality.Tiers[0]
		_, err = s.internalExecutor.Exec(ctx, "update-system-locations", nil,
			stmt, tier.Key, tier.Value, loc.Latitude, loc.Longitude)
		if err != nil {
			return err
		}
	}
	return nil
}

// retireOldTsSettings copies deprecated ts settings to their new names.
func (s *Server) retireOldTsSettings(ctx context.Context) error {
	_, err := s.internalExecutor.Exec(ctx, "copy-setting", nil, /* txn */ `
INSERT INTO system.settings (name, value, "lastUpdated", "valueType")
   SELECT 'timeseries.storage.resolution_10s.ttl', value, "lastUpdated", "valueType"
     FROM system.settings WHERE name = 'timeseries.storage.10s_resolution_ttl'
ON CONFLICT (name) DO NOTHING`)
	if err != nil {
		return err
	}
	_, err = s.internalExecutor.Exec(ctx, "copy-setting", nil, /* txn */ `
INSERT INTO system.settings (name, value, "lastUpdated", "valueType")
   SELECT 'timeseries.storage.resolution_30m.ttl', value, "lastUpdated", "valueType"
     FROM system.settings WHERE name = 'timeseries.storage.30m_resolution_ttl'
ON CONFLICT (name) DO NOTHING`)
	return err
}

// depublicizeSystemComments revokes mistaken public privileges.
func (s *Server) depublicizeSystemComments(ctx context.Context) error {
	for _, priv := range []string{"GRANT", "INSERT", "DELETE", "UPDATE"} {
		stmt := fmt.Sprintf(`REVOKE %s ON TABLE system.comments FROM public`, priv)
		_, err := s.internalExecutor.Exec(ctx, "depublicize-system-comments", nil, stmt)
		if err != nil {
			return err
		}
	}
	return nil
}
