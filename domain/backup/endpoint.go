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

// Package backup handle data backup/restore to/from ZIP format.
package backup

// Documize data is all held in the SQL database in relational format.
// The objective is to export the data into a compressed file that
// can be restored again as required.
//
// This allows for the following scenarios to be supported:
//
// 1. Copying data from one Documize instance to another.
// 2. Changing database provider (e.g. from MySQL to PostgreSQL).
// 3. Moving between Documize Cloud and self-hosted instances.
// 4. GDPR compliance (send copy of data and nuke whatever remains).
// 5. Setting up sample Documize instance with pre-defined content.
//
// The initial implementation is restricted to tenant or global
// backup/restore operations and can only be performed by a verified
// Global Administrator.
//
// In future the process should be able to support per space backup/restore
// operations. This is subject to further review.

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/documize/community/core/env"
	"github.com/documize/community/core/response"
	"github.com/documize/community/core/streamutil"
	"github.com/documize/community/domain"
	indexer "github.com/documize/community/domain/search"
	"github.com/documize/community/domain/store"
	m "github.com/documize/community/model/backup"
)

// Handler contains the runtime information such as logging and database.
type Handler struct {
	Runtime *env.Runtime
	Store   *store.Store
	Indexer indexer.Indexer
}

// Backup generates binary file of all instance settings and contents.
// The content is pulled directly from the database and marshalled to JSON.
// A zip file is then sent to the caller.
func (h *Handler) Backup(w http.ResponseWriter, r *http.Request) {
	method := "system.backup"
	ctx := domain.GetRequestContext(r)

	if !ctx.Administrator {
		response.WriteForbiddenError(w)
		h.Runtime.Log.Info(fmt.Sprintf("Non-admin attempted system backup operation (user ID: %s)", ctx.UserID))
		return
	}

	defer streamutil.Close(r.Body)
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		response.WriteBadRequestError(w, method, err.Error())
		h.Runtime.Log.Error(method, err)
		return
	}

	spec := m.ExportSpec{}
	err = json.Unmarshal(body, &spec)
	if err != nil {
		response.WriteBadRequestError(w, method, err.Error())
		h.Runtime.Log.Error(method, err)
		return
	}

	h.Runtime.Log.Info("Backup started")

	bh := backerHandler{Runtime: h.Runtime, Store: h.Store, Context: ctx, Spec: spec}

	// Produce zip file on disk.
	filename, err := bh.GenerateBackup()
	if err != nil {
		response.WriteServerError(w, method, err)
		h.Runtime.Log.Error(method, err)
		return
	}

	// Read backup file into memory.
	// DEBT: write file directly to HTTP response stream?
	bk, err := ioutil.ReadFile(filename)
	if err != nil {
		response.WriteServerError(w, method, err)
		h.Runtime.Log.Error(method, err)
		return
	}

	// Standard HTTP headers.
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`" ; `+`filename*="`+filename+`"`)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(bk)))
	// Custom HTTP header helps API consumer to extract backup filename cleanly
	// instead of parsing 'Content-Disposition' header.
	// This HTTP header is CORS white-listed.
	w.Header().Set("x-documize-filename", filename)

	// Write backup to response stream.
	x, err := w.Write(bk)
	if err != nil {
		response.WriteServerError(w, method, err)
		h.Runtime.Log.Error(method, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	h.Runtime.Log.Info(fmt.Sprintf("Backup completed for %s by %s, size %d", ctx.OrgID, ctx.UserID, x))

	// Delete backup file if not requested to keep it.
	if !spec.Retain {
		os.Remove(filename)
	}
}
