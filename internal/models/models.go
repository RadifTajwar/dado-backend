// Package models holds the records stored in MongoDB. The json tags are the
// API contract (frontend/lib/types.ts): camelCase, IDs as hex strings.
package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// ServiceFields are the parts of a service an admin edits. The same struct is
// the request body for create and update, so the two can never drift apart.
type ServiceFields struct {
	Name       string  `bson:"name" json:"name"`
	Discipline string  `bson:"discipline" json:"discipline"` // photo | video | voice
	Group      string  `bson:"group" json:"group"`
	Use        string  `bson:"use" json:"use"`
	Rate       float64 `bson:"rate" json:"rate"`
	Unit       string  `bson:"unit" json:"unit"`
	Turnaround string  `bson:"turnaround" json:"turnaround"`
	Featured   bool    `bson:"featured" json:"featured"`
	Active     bool    `bson:"active" json:"active"`
	Order      int     `bson:"order" json:"order"`
}

type Service struct {
	ID            bson.ObjectID `bson:"_id" json:"id"`
	ServiceFields `bson:",inline"`
	CreatedAt     time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt     time.Time `bson:"updatedAt" json:"updatedAt"`
}

// MediaImage is one fixed image slot on the public site; the slot is the _id.
type MediaImage struct {
	Slot      string    `bson:"_id" json:"slot"` // hero | about | photo | video | voice
	URL       string    `bson:"url" json:"url"`
	Alt       string    `bson:"alt" json:"alt"`
	UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt"`
}

type PortfolioFields struct {
	Title     string `bson:"title" json:"title"`
	Label     string `bson:"label" json:"label"`
	Alt       string `bson:"alt" json:"alt"`
	BeforeURL string `bson:"beforeUrl" json:"beforeUrl"`
	AfterURL  string `bson:"afterUrl" json:"afterUrl"`
	Order     int    `bson:"order" json:"order"`
	Active    bool   `bson:"active" json:"active"`
}

// PortfolioItem is a before/after pair for the "Recent work" sliders.
type PortfolioItem struct {
	ID              bson.ObjectID `bson:"_id" json:"id"`
	PortfolioFields `bson:",inline"`
	CreatedAt       time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt       time.Time `bson:"updatedAt" json:"updatedAt"`
}

type Contact struct {
	ID            bson.ObjectID `bson:"_id" json:"id"`
	Name          string        `bson:"name" json:"name"`
	Email         string        `bson:"email" json:"email"`
	Phone         string        `bson:"phone" json:"phone"`
	Service       string        `bson:"service" json:"service"`
	Message       string        `bson:"message" json:"message"`
	Status        string        `bson:"status" json:"status"` // new | replied | archived | flagged
	FlagReason    string        `bson:"flagReason" json:"flagReason"`
	Read          bool          `bson:"read" json:"read"`
	LastEmailedAt *time.Time    `bson:"lastEmailedAt" json:"lastEmailedAt"` // null until emailed
	CreatedAt     time.Time     `bson:"createdAt" json:"createdAt"`
}

type Trial struct {
	ID            bson.ObjectID  `bson:"_id" json:"id"`
	Name          string         `bson:"name" json:"name"`
	Email         string         `bson:"email" json:"email"`
	Company       string         `bson:"company" json:"company"`
	Phone         string         `bson:"phone" json:"phone"`
	Services      []string       `bson:"services" json:"services"` // photo | video | voice
	Link          string         `bson:"link" json:"link"`
	Volume        string         `bson:"volume" json:"volume"`
	Deadline      string         `bson:"deadline" json:"deadline"`
	Brief         string         `bson:"brief" json:"brief"`
	NDA           bool           `bson:"nda" json:"nda"`
	Status        string         `bson:"status" json:"status"` // new | converted | declined | flagged
	FlagReason    string         `bson:"flagReason" json:"flagReason"`
	Read          bool           `bson:"read" json:"read"`
	LastEmailedAt *time.Time     `bson:"lastEmailedAt" json:"lastEmailedAt"`
	ClientID      *bson.ObjectID `bson:"clientId" json:"clientId"` // set once converted
	CreatedAt     time.Time      `bson:"createdAt" json:"createdAt"`
}

type ClientFields struct {
	Name    string `bson:"name" json:"name"`
	Company string `bson:"company" json:"company"`
	Email   string `bson:"email" json:"email"`
	Phone   string `bson:"phone" json:"phone"`
	Country string `bson:"country" json:"country"`
	Notes   string `bson:"notes" json:"notes"`
	Since   string `bson:"since" json:"since"`   // YYYY-MM-DD
	Source  string `bson:"source" json:"source"` // trial | direct
}

type Client struct {
	ID           bson.ObjectID `bson:"_id" json:"id"`
	ClientFields `bson:",inline"`
	CreatedAt    time.Time `bson:"createdAt" json:"createdAt"`
}

type JobFields struct {
	ClientID  bson.ObjectID `bson:"clientId" json:"clientId"`
	ServiceID bson.ObjectID `bson:"serviceId" json:"serviceId"`
	Date      string        `bson:"date" json:"date"` // YYYY-MM-DD
	Qty       int           `bson:"qty" json:"qty"`
	Amount    float64       `bson:"amount" json:"amount"` // what was charged
	Paid      float64       `bson:"paid" json:"paid"`     // received so far
	Status    string        `bson:"status" json:"status"` // quoted | in-progress | delivered
	Details   string        `bson:"details" json:"details"`
	Link      string        `bson:"link" json:"link"`
}

// Job is one piece of billed work for a client ("works" in the prototype).
type Job struct {
	ID        bson.ObjectID `bson:"_id" json:"id"`
	JobFields `bson:",inline"`
	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
}

// Admin is a person who can sign in. Nothing secret ever leaves as JSON.
type Admin struct {
	ID             bson.ObjectID `bson:"_id" json:"-"`
	Email          string        `bson:"email" json:"-"`
	PasswordHash   string        `bson:"passwordHash" json:"-"`
	ResetTokenHash string        `bson:"resetTokenHash,omitempty" json:"-"`
	ResetExpires   *time.Time    `bson:"resetExpires,omitempty" json:"-"`
	CreatedAt      time.Time     `bson:"createdAt" json:"-"`
}

// AdminUser is the public view of an admin (GET /api/auth/me).
type AdminUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// Dhaka is the studio's time zone. Bangladesh has no daylight saving, so a
// fixed offset is exact and needs no tzdata on the server.
var Dhaka = time.FixedZone("Asia/Dhaka", 6*60*60)

// Now is the current time as MongoDB stores it (UTC, millisecond precision),
// so a value looks the same before and after a round trip.
func Now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

// Today is the current calendar day in Dhaka as "YYYY-MM-DD".
func Today() string { return time.Now().In(Dhaka).Format(time.DateOnly) }
