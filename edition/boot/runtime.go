// Copyright 2016 Documize Inc. <legal@documize.com>. All rights reserved.
//
// This software (Documize Community Edition) is licensed under
// GNU AGPL v3 http://www.gnu.org/licenses/agpl-3.0.en.html
//
// You can operate outside the AGPL restrictions by purchasing
// Documize Enterprise Edition and obtaining a commercial license
// by contacting <sales@documize.com>.
//
// https://documize.com

// Package boot prepares runtime environment.
package boot

import (
	"time"

	"github.com/documize/community/core/database"
	"github.com/documize/community/core/env"
	"github.com/documize/community/core/secrets"
	"github.com/documize/community/domain"
	"github.com/documize/community/edition/storage"
	"github.com/jmoiron/sqlx"
)

// InitRuntime prepares runtime using command line and environment variables.
func InitRuntime(r *env.Runtime, s *domain.Store) bool {
	// We need SALT to hash auth JWT tokens
	if r.Flags.Salt == "" {
		r.Flags.Salt = secrets.RandSalt()

		if r.Flags.Salt == "" {
			return false
		}

		r.Log.Info("please set DOCUMIZESALT or use -salt with this value: " + r.Flags.Salt)
	}

	// We can use either or both HTTP and HTTPS ports
	if r.Flags.SSLCertFile == "" && r.Flags.SSLKeyFile == "" {
		if r.Flags.HTTPPort == "" {
			r.Flags.HTTPPort = "80"
		}
	} else {
		if r.Flags.HTTPPort == "" {
			r.Flags.HTTPPort = "443"
		}
	}

	// Set up required storage provider.
	switch r.Flags.DBType {
	case "mysql":
		storage.SetMySQLProvider(r, s)
	case "mariadb":
		storage.SetMySQLProvider(r, s)
	case "percona":
		storage.SetMySQLProvider(r, s)
	case "pggg":
		// storage.SetPostgresSQLProvider(r, s)
	case "mssql":
		// storage.SetSQLServerProvider(r, s)
	}

	// Open connection to database
	db, err := sqlx.Open(r.StoreProvider.DriverName(), r.StoreProvider.MakeConnectionString()) //r.Flags.DBConn
	if err != nil {
		r.Log.Error("unable to setup database", err)
	}

	// Database handle
	r.Db = db

	// Database connection defaults
	r.Db.SetMaxIdleConns(30)
	r.Db.SetMaxOpenConns(100)
	r.Db.SetConnMaxLifetime(time.Second * 14400)

	// Database good?
	err = r.Db.Ping()
	if err != nil {
		r.Log.Error("unable to connect to database - "+r.StoreProvider.Example(), err)
		return false
	}

	// Check database and upgrade if required.
	if r.Flags.SiteMode != env.SiteModeOffline {
		if database.Check(r) {
			if err := database.InstallUpgrade(r, true); err != nil {
				r.Log.Error("unable to run database migration", err)
				return false
			}
		}
	}

	return true
}

// Clever way to detect database type:
// https://github.com/golang-sql/sqlexp/blob/c2488a8be21d20d31abf0d05c2735efd2d09afe4/quoter.go#L46
