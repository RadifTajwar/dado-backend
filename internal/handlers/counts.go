package handlers

import (
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// GET /api/admin/counts — the numbers on the sidebar badges.
func (h *Handler) Counts(w http.ResponseWriter, r *http.Request) {
	out := map[string]int64{}
	for key, q := range map[string]struct {
		coll   *mongo.Collection
		filter bson.M
	}{
		"clients":        {h.db.Clients, bson.M{}},
		"unreadTrials":   {h.db.Trials, bson.M{"read": false}},
		"unreadContacts": {h.db.Contacts, bson.M{"read": false}},
		"activeServices": {h.db.Services, bson.M{"active": true}},
	} {
		n, err := q.coll.CountDocuments(r.Context(), q.filter)
		if err != nil {
			writeServerError(w, r, err)
			return
		}
		out[key] = n
	}
	writeJSON(w, http.StatusOK, out)
}
