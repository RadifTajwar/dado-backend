// Package db owns the MongoDB connection and the collection handles.
package db

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

type DB struct {
	client *mongo.Client

	Admins    *mongo.Collection
	Services  *mongo.Collection
	Clients   *mongo.Collection
	Jobs      *mongo.Collection
	Trials    *mongo.Collection
	Contacts  *mongo.Collection
	Portfolio *mongo.Collection
	Media     *mongo.Collection
}

// Connect opens the connection, checks it with a ping and makes sure the
// indexes exist.
func Connect(ctx context.Context, uri, name string) (*DB, error) {
	client, err := mongo.Connect(options.Client().
		ApplyURI(uri).
		SetServerSelectionTimeout(10 * time.Second).
		SetTimeout(15 * time.Second)) // upper bound for every single operation
	if err != nil {
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, readpref.Primary()); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("ping failed (is this machine's IP allowed in Atlas → Network Access?): %w", err)
	}

	d := client.Database(name)
	db := &DB{
		client:    client,
		Admins:    d.Collection("admins"),
		Services:  d.Collection("services"),
		Clients:   d.Collection("clients"),
		Jobs:      d.Collection("jobs"),
		Trials:    d.Collection("trials"),
		Contacts:  d.Collection("contacts"),
		Portfolio: d.Collection("portfolio"),
		Media:     d.Collection("media"),
	}
	return db, db.ensureIndexes(ctx)
}

func (db *DB) Close(ctx context.Context) error { return db.client.Disconnect(ctx) }

func (db *DB) ensureIndexes(ctx context.Context) error {
	indexes := map[*mongo.Collection][]mongo.IndexModel{
		db.Admins:   {{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true)}},
		db.Services: {{Keys: bson.D{{Key: "discipline", Value: 1}, {Key: "order", Value: 1}}}},
		db.Jobs:     {{Keys: bson.D{{Key: "clientId", Value: 1}}}},
		db.Trials:   {{Keys: bson.D{{Key: "createdAt", Value: -1}}}},
		db.Contacts: {{Keys: bson.D{{Key: "createdAt", Value: -1}}}},
	}
	for coll, models := range indexes {
		if _, err := coll.Indexes().CreateMany(ctx, models); err != nil {
			return fmt.Errorf("create indexes on %s: %w", coll.Name(), err)
		}
	}
	return nil
}

// FindAll runs a query and returns every match. It returns an empty slice,
// never nil, so lists always encode as [] in JSON.
func FindAll[T any](ctx context.Context, coll *mongo.Collection, filter any, sort bson.D) ([]T, error) {
	cur, err := coll.Find(ctx, filter, options.Find().SetSort(sort))
	if err != nil {
		return nil, err
	}
	out := []T{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}
