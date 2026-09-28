// Package seed fills an empty database with what the site needs to work: the
// first admin, the published price list and the starter images. It runs on
// every start and only ever writes to collections that are still empty.
package seed

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"dado/internal/auth"
	"dado/internal/config"
	"dado/internal/db"
	"dado/internal/models"
)

// Run creates whatever is missing. Safe to call on every start.
func Run(ctx context.Context, d *db.DB, cfg *config.Config) error {
	steps := []struct {
		name string
		coll *mongo.Collection
		docs func() ([]any, error)
	}{
		{"admin", d.Admins, func() ([]any, error) { return adminDocs(cfg) }},
		{"services", d.Services, func() ([]any, error) { return serviceDocs(), nil }},
		{"media", d.Media, func() ([]any, error) { return mediaDocs(), nil }},
		{"portfolio", d.Portfolio, func() ([]any, error) { return portfolioDocs(), nil }},
	}
	for _, s := range steps {
		if err := insertIfEmpty(ctx, s.coll, s.name, s.docs); err != nil {
			return err
		}
	}
	return nil
}

func insertIfEmpty(ctx context.Context, coll *mongo.Collection, name string, docs func() ([]any, error)) error {
	n, err := coll.EstimatedDocumentCount(ctx)
	if err != nil || n > 0 {
		return err
	}
	list, err := docs()
	if err != nil {
		return fmt.Errorf("seed %s: %w", name, err)
	}
	if _, err := coll.InsertMany(ctx, list); err != nil {
		return fmt.Errorf("seed %s: %w", name, err)
	}
	slog.Info("seeded", "collection", name, "documents", len(list))
	return nil
}

/* ---------- admin ---------- */

func adminDocs(cfg *config.Config) ([]any, error) {
	if cfg.AdminEmail == "" || len(cfg.AdminPassword) < 8 {
		return nil, fmt.Errorf("no admin yet: set ADMIN_EMAIL and ADMIN_PASSWORD (8+ characters) in .env")
	}
	hash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		return nil, err
	}
	return []any{models.Admin{ID: bson.NewObjectID(), Email: cfg.AdminEmail, PasswordHash: hash, CreatedAt: models.Now()}}, nil
}

/* ---------- services: the published price list ---------- */

// The price list as it appears on the Services page, table by table.
var priceList = []struct {
	discipline, group string
	rows              [][3]string // name, typical use, unit ("" = the discipline's usual unit)
	rates             []float64
}{
	{"photo", "Cut-out & path", [][3]string{
		{"Basic clipping path", "Simple shapes, hard edges"},
		{"Simple clipping path", "A few interior curves"},
		{"Medium clipping path", "Handles, straps, gaps"},
		{"Complex clipping path", "Chains, mesh, spokes"},
		{"Super complex path", "Lace, wire, dense cut-outs"},
		{"Remove unwanted objects", "Tags, stands, distractions"},
	}, []float64{0.30, 0.45, 1.00, 2.50, 4.00, 1.00}},
	{"photo", "Masking, shadow & reflection", [][3]string{
		{"Product or object masking", "Soft-edged subjects"},
		{"Hair & fur masking", "Model and pet shots"},
		{"Transparent / alpha masking", "Glass, bottles, acrylic"},
		{"Natural drop shadow", "Grounded product on white"},
		{"Reflection creation", "Footwear, tech, glassware"},
		{"Retain or rebuild original shadow", "Keeping the set lighting"},
	}, []float64{1.00, 2.00, 1.50, 0.65, 0.65, 0.50}},
	{"photo", "Retouch, colour & apparel", [][3]string{
		{"Colour correction & exposure", "Batch-matching a shoot"},
		{"Colourway / variant creation", "One shot, many SKUs"},
		{"Ghost mannequin, neck joint", "Apparel on white"},
		{"Ghost mannequin, full + liquify", "Neck, sleeves, wrinkle clean"},
		{"Skin retouch", "Ecommerce model shots"},
		{"Beauty / editorial retouch", "Campaign and cover work"},
		{"Jewellery retouch & enhancement", "Stones, metal, reflections"},
		{"Photo restoration", "Damaged, faded, torn originals"},
	}, []float64{1.00, 2.00, 1.50, 3.00, 1.00, 3.00, 3.00, 10.00}},
	{"video", "Edit & finish", [][3]string{
		{"Assembly edit from rushes", "Rough cut to your script"},
		{"Social cut-down + captions", "Reels, Shorts, TikTok"},
		{"Colour grade", "Shot-matching and look"},
		{"Product clean-up & rotoscoping", "Rigs, logos, blemishes out"},
		{"Motion graphics & lower thirds", "Titles, callouts, end cards"},
		{"Subtitles & burn-in", "Per language, per minute"},
		{"Extra aspect-ratio deliverable", "9:16, 1:1, 4:5 from a 16:9"},
		{"Audio sweetening & mix", "Dialogue clean, levels, music"},
	}, []float64{22.00, 18.00, 25.00, 45.00, 35.00, 6.00, 8.00, 14.00}},
	{"voice", "Voice work", [][3]string{
		{"Commercial read", "Ads, promos, product films"},
		{"Explainer & corporate narration", "Brand and internal video"},
		{"E-learning narration", "Course modules, long-form"},
		{"IVR & phone prompts", "Per prompt", "prompt"},
		{"Audiobook / long-form", "Finished-hour rate", "finished hour"},
		{"Sync to picture", "Per finished minute", "minute"},
		{"Mixing & mastering", "Per finished minute", "minute"},
		{"Noise repair & restoration", "Per finished minute", "minute"},
	}, []float64{12.00, 9.00, 7.00, 4.00, 180.00, 10.00, 8.00, 12.00}},
}

var (
	usualUnit       = map[string]string{"photo": "image", "video": "minute", "voice": "100 words"}
	usualTurnaround = map[string]string{"photo": "24 hours", "video": "48 hours", "voice": "24 hours"}

	// the "most-ordered" rows on the home page
	featured = map[string]bool{
		"Basic clipping path": true, "Natural drop shadow": true, "Ghost mannequin, neck joint": true,
		"Skin retouch": true, "Social cut-down + captions": true, "Colour grade": true, "Commercial read": true,
	}
)

func serviceDocs() []any {
	var docs []any
	now := models.Now()
	order := map[string]int{}
	for _, table := range priceList {
		for i, row := range table.rows {
			name, unit, turnaround := row[0], row[2], usualTurnaround[table.discipline]
			if unit == "" {
				unit = usualUnit[table.discipline]
			}
			if name == "Basic clipping path" {
				turnaround = "12 hours"
			}
			order[table.discipline]++
			docs = append(docs, models.Service{
				ID: bson.NewObjectID(),
				ServiceFields: models.ServiceFields{
					Name: name, Discipline: table.discipline, Group: table.group, Use: row[1],
					Rate: table.rates[i], Unit: unit, Turnaround: turnaround,
					Featured: featured[name], Active: true, Order: order[table.discipline],
				},
				CreatedAt: now, UpdatedAt: now,
			})
		}
	}
	return docs
}

/* ---------- starter images (replace them from the admin) ---------- */

func unsplash(id string) string {
	return "https://images.unsplash.com/" + id + "?auto=format&fit=crop&w=1600&q=80"
}

func mediaDocs() []any {
	now := models.Now()
	img := func(slot, id, alt string) any {
		return models.MediaImage{Slot: slot, URL: unsplash(id), Alt: alt, UpdatedAt: now}
	}
	return []any{
		img("hero", "photo-1483985988355-763728e1935b", "Apparel on a rail in a studio set, lit for a product shoot"),
		img("about", "photo-1526170375885-4d8ecf77b99f", "Camera on a tripod in a working studio space"),
		img("photo", "photo-1542291026-7eec264c27ff", "Red trainer photographed on a plain studio background"),
		img("video", "photo-1492691527719-9d1e07e534b4", "Operator framing a shot on a cinema camera"),
		img("voice", "photo-1590602847861-f357a9332bbc", "Condenser microphone in a treated recording booth"),
	}
}

func portfolioDocs() []any {
	now := models.Now()
	pair := func(order int, title, label, alt, id string) any {
		after := unsplash(id)
		return models.PortfolioItem{
			ID: bson.NewObjectID(),
			PortfolioFields: models.PortfolioFields{
				Title: title, Label: label, Alt: alt,
				// The demo "before" is the same photo, desaturated and flattened by
				// the image CDN, so the slider shows a real difference.
				BeforeURL: after + "&sat=-100&con=-18&bri=8", AfterURL: after,
				Order: order, Active: true,
			},
			CreatedAt: now, UpdatedAt: now,
		}
	}
	return []any{
		pair(1, "Model & skin retouch", "Photo", "Studio portrait of a model", "photo-1524504388940-b1c1722653e1"),
		pair(2, "Jewellery clean-up", "Photo", "Jewellery on a display stand", "photo-1515562141207-7a88fb7ce338"),
		pair(3, "Colour grade", "Video frame", "Product scene in a shop", "photo-1441986300917-64674bd600d8"),
	}
}

// day turns a prototype date ("2026-09-25") into a timestamp at noon in Dhaka.
func day(date string) time.Time {
	t, _ := time.ParseInLocation(time.DateOnly, date, models.Dhaka)
	return t.Add(12 * time.Hour).UTC()
}
