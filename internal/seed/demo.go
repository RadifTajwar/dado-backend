package seed

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"dado/internal/db"
	"dado/internal/models"
)

/* Demo data: the prototype's sample clients, jobs, trials and contacts
   (prototype/admin/assets/js/store.js), so the admin has something to show.
   Loaded only with `go run ./cmd/api -seed-demo`, and only into a database
   where all four collections are still empty, so it never mixes with real
   records. To remove it later, drop those four collections in Atlas. */

// Prototype service names → the matching row in the published price list.
var demoService = map[string]string{
	"s1": "Basic clipping path", "s2": "Natural drop shadow", "s3": "Ghost mannequin, neck joint",
	"s4": "Skin retouch", "s5": "Jewellery retouch & enhancement", "s6": "Colour correction & exposure",
	"s7": "Hair & fur masking", "s8": "Photo restoration", "s9": "Social cut-down + captions",
	"s10": "Colour grade", "s11": "Motion graphics & lower thirds", "s12": "Product clean-up & rotoscoping",
	"s13": "Subtitles & burn-in", "s14": "Commercial read", "s15": "E-learning narration", "s16": "Mixing & mastering",
}

var demoClients = [][6]string{ // name, company, email, country, since, source
	{"Nora Beckett", "Halden & Co", "nora@haldenco.com", "United Kingdom", "2025-11-04", "trial"},
	{"Tomas Aleksic", "Vireo Activewear", "tomas@vireo.store", "Germany", "2026-01-19", "trial"},
	{"Priya Raghavan", "Lumen Jewellery", "priya@lumen-jewels.com", "Singapore", "2026-02-08", "direct"},
	{"Daniel Okafor", "Okafor Studio", "dan@okaforstudio.co", "Nigeria", "2026-03-22", "trial"},
	{"Marta Ruiz", "Casa Verde Home", "marta@casaverde.es", "Spain", "2026-04-15", "direct"},
	{"Kenji Watanabe", "Aoi Optics", "kenji@aoioptics.jp", "Japan", "2026-05-30", "trial"},
	{"Sarah Lindqvist", "Nord Pack", "sarah@nordpack.se", "Sweden", "2026-06-11", "direct"},
	{"Adam Feldman", "Feldman Media", "adam@feldmanmedia.us", "United States", "2026-07-02", "trial"},
	{"Leila Haddad", "Atlas Travel Co", "leila@atlastravel.ae", "UAE", "2026-08-20", "direct"},
}

type demoJob struct {
	client, service, date string
	qty                   int
	amount, paid          float64
	status                string
}

var demoJobs = []demoJob{
	{"c1", "s1", "2026-03-04", 1200, 360, 360, "delivered"}, {"c1", "s2", "2026-03-27", 640, 416, 416, "delivered"},
	{"c1", "s4", "2026-04-18", 310, 310, 310, "delivered"}, {"c1", "s3", "2026-05-09", 280, 420, 420, "delivered"},
	{"c1", "s2", "2026-06-14", 900, 585, 585, "delivered"}, {"c1", "s6", "2026-07-21", 450, 450, 300, "delivered"},
	{"c1", "s1", "2026-08-30", 2100, 630, 0, "delivered"}, {"c1", "s4", "2026-09-16", 380, 380, 0, "in-progress"},
	{"c2", "s3", "2026-03-12", 210, 315, 315, "delivered"}, {"c2", "s1", "2026-04-02", 1600, 480, 480, "delivered"},
	{"c2", "s9", "2026-05-20", 14, 252, 252, "delivered"}, {"c2", "s3", "2026-06-25", 340, 510, 510, "delivered"},
	{"c2", "s10", "2026-08-07", 9, 225, 120, "delivered"}, {"c2", "s1", "2026-09-11", 1850, 555, 0, "delivered"},
	{"c3", "s5", "2026-02-21", 120, 360, 360, "delivered"}, {"c3", "s5", "2026-04-09", 260, 780, 780, "delivered"},
	{"c3", "s7", "2026-05-16", 140, 280, 280, "delivered"}, {"c3", "s5", "2026-07-08", 410, 1230, 800, "delivered"},
	{"c3", "s2", "2026-09-03", 520, 338, 0, "delivered"}, {"c4", "s4", "2026-04-05", 190, 190, 190, "delivered"},
	{"c4", "s6", "2026-06-02", 300, 300, 300, "delivered"}, {"c4", "s4", "2026-08-13", 240, 240, 0, "delivered"},
	{"c5", "s2", "2026-05-01", 380, 247, 247, "delivered"}, {"c5", "s6", "2026-06-19", 420, 420, 420, "delivered"},
	{"c5", "s2", "2026-08-24", 610, 397, 200, "delivered"}, {"c6", "s1", "2026-06-08", 940, 282, 282, "delivered"},
	{"c6", "s7", "2026-07-14", 160, 320, 320, "delivered"}, {"c6", "s1", "2026-09-05", 1300, 390, 0, "in-progress"},
	{"c7", "s11", "2026-06-27", 6, 210, 210, "delivered"}, {"c7", "s9", "2026-07-30", 11, 198, 198, "delivered"},
	{"c7", "s14", "2026-09-09", 24, 288, 0, "delivered"}, {"c8", "s10", "2026-07-11", 16, 400, 400, "delivered"},
	{"c8", "s12", "2026-08-05", 7, 315, 315, "delivered"}, {"c8", "s11", "2026-09-19", 12, 420, 0, "in-progress"},
	{"c9", "s15", "2026-08-28", 38, 266, 266, "delivered"}, {"c9", "s16", "2026-09-21", 22, 176, 0, "quoted"},
}

type demoTrial struct {
	name, company, email string
	services             []string
	link, volume, date   string
	status, client       string // client: prototype id when converted
}

var demoTrials = []demoTrial{
	{"Grace Mbeki", "Sunda Ceramics", "grace@sundaceramics.com", []string{"photo"}, "https://drive.google.com/drive/folders/1aB9xKp", "100–500", "2026-09-25", "new", ""},
	{"Viktor Novak", "Halo Furniture", "v.novak@halofurn.cz", []string{"photo", "video"}, "https://www.dropbox.com/scl/fo/8qm2", "Under 100", "2026-09-24", "new", ""},
	{"—", "", "asdf@asdf.asdf", []string{"photo"}, "https://drive.google.com/file/d/xxx", "1–5", "2026-09-24", "flagged", ""},
	{"Amara Diallo", "Kora Textiles", "amara@koratextiles.sn", []string{"photo"}, "https://wetransfer.com/downloads/93kf", "500–2,000", "2026-09-22", "new", ""},
	{"Leila Haddad", "Atlas Travel Co", "leila@atlastravel.ae", []string{"video", "voice"}, "https://drive.google.com/drive/folders/7Yt2mQ", "100–500", "2026-08-18", "converted", "c9"},
	{"test test", "", "noreply@mailinator.com", []string{"video"}, "https://drive.google.com/drive/folders/zzz", "1–5", "2026-08-16", "flagged", ""},
	{"Adam Feldman", "Feldman Media", "adam@feldmanmedia.us", []string{"video"}, "https://frame.io/p/9xkq", "100–500", "2026-06-28", "converted", "c8"},
	{"Ines Moreau", "Brut Parfums", "ines@brutparfums.fr", []string{"photo", "voice"}, "https://1drv.ms/f/s!ApQ3", "Under 100", "2026-09-20", "new", ""},
	{"Kenji Watanabe", "Aoi Optics", "kenji@aoioptics.jp", []string{"photo"}, "https://drive.google.com/drive/folders/4Lm8", "500–2,000", "2026-05-26", "converted", "c6"},
	{"Oliver Grant", "Grant & Sons", "oliver@grantandsons.co.uk", []string{"photo"}, "https://www.dropbox.com/scl/fo/pp41", "2,000+ / ongoing catalogue", "2026-09-18", "new", ""},
	{"Rui Santos", "Vela Surf", "rui@velasurf.pt", []string{"video"}, "https://mega.nz/folder/3kQ", "Under 100", "2026-09-12", "declined", ""},
}

var demoContacts = [][6]string{ // name, email, service, date, status, message
	{"Hannah Weiss", "hannah@studioweiss.de", "Ghost mannequin & liquify", "2026-09-26", "new", "We shoot roughly 400 apparel SKUs a month. Can you hold a fixed look across a season?"},
	{"Marco Bianchi", "marco@bianchiferro.it", "Colour grading", "2026-09-25", "new", "Six brand films to grade before a trade show on the 14th. Is that feasible?"},
	{"Aisha Rahman", "aisha@thelittlelabel.co", "Clipping path & background removal", "2026-09-23", "replied", "What is the per-image rate at around 3,000 images a month?"},
	{"Peter Sandstrom", "peter@sandstrom.photo", "Model / beauty retouching", "2026-09-21", "replied", "Do you white-label? My clients should not see a third party in the chain."},
	{"Yuki Tanaka", "yuki@mochidesign.jp", "E-learning narration", "2026-09-19", "new", "Need Japanese and English narration for 12 course modules. Same voice for both?"},
	{"Claire Dubois", "claire@maisonclaire.fr", "Something else / not sure yet", "2026-09-15", "archived", "Looking for someone to own all post for our SS27 campaign. Who should I talk to?"},
	{"Ben Carter", "ben@cartercycles.com", "Masking, shadow & reflection", "2026-09-12", "replied", "Bike frames on white with a real shadow. Can you match our existing catalogue?"},
	{"Fatima Zahra", "fatima@zahraatelier.ma", "Jewellery & product retouching", "2026-09-08", "archived", "Gold and stones, about 90 pieces. Sent a sample last week — any update?"},
	{"qqq", "qqq@qqq.qq", "Something else / not sure yet", "2026-09-24", "flagged", "how much"},
	{"Marketing Team", "no-reply@bulkmailer.biz", "Clipping path & background removal", "2026-09-17", "flagged", "GROW YOUR BUSINESS 10X with our SEO package. Reply STOP to opt out."},
}

// Demo loads the sample data if clients, jobs, trials and contacts are all empty.
func Demo(ctx context.Context, d *db.DB) error {
	for _, coll := range []*mongo.Collection{d.Clients, d.Jobs, d.Trials, d.Contacts} {
		n, err := coll.EstimatedDocumentCount(ctx)
		if err != nil {
			return err
		}
		if n > 0 {
			slog.Info("demo data skipped: collection is not empty", "collection", coll.Name())
			return nil
		}
	}

	services, err := db.FindAll[models.Service](ctx, d.Services, bson.M{}, bson.D{})
	if err != nil {
		return err
	}
	serviceID := map[string]bson.ObjectID{}
	for _, s := range services {
		serviceID[s.Name] = s.ID
	}

	clientID := map[string]bson.ObjectID{}
	var clients []any
	for i, c := range demoClients {
		key := "c" + strconv.Itoa(i+1)
		clientID[key] = bson.NewObjectID()
		clients = append(clients, models.Client{
			ID: clientID[key],
			ClientFields: models.ClientFields{
				Name: c[0], Company: c[1], Email: c[2], Country: c[3], Since: c[4], Source: c[5],
				Phone: fmt.Sprintf("+%d %d %d", 20+i, 700+i*13, 100000+i*7919), // same fake numbers as the prototype
			},
			CreatedAt: day(c[4]),
		})
	}

	var jobs []any
	for _, j := range demoJobs {
		sid, ok := serviceID[demoService[j.service]]
		if !ok {
			return fmt.Errorf("demo data: no service named %q", demoService[j.service])
		}
		jobs = append(jobs, models.Job{
			ID: bson.NewObjectID(),
			JobFields: models.JobFields{
				ClientID: clientID[j.client], ServiceID: sid, Date: j.date,
				Qty: j.qty, Amount: j.amount, Paid: j.paid, Status: j.status,
			},
			CreatedAt: day(j.date),
		})
	}

	var trials []any
	for i, t := range demoTrials {
		trial := models.Trial{
			ID: bson.NewObjectID(), Name: t.name, Company: t.company, Email: t.email,
			Services: t.services, Link: t.link, Volume: t.volume,
			Deadline: "Standard — 24h photo / voice, 48h video",
			Brief:    "Cut out on pure white, natural shadow, colour matched to the swatch in the folder. Export 2000px square JPG named by SKU.",
			NDA:      i%4 == 0, Status: t.status, Read: t.status != "new", CreatedAt: day(t.date),
		}
		if t.status == "flagged" {
			trial.FlagReason = "Disposable inbox, no company, placeholder name."
			if i == 2 {
				trial.FlagReason = "Email bounces — no way to reach them."
			}
		}
		if t.client != "" {
			id := clientID[t.client]
			trial.ClientID = &id
		}
		trials = append(trials, trial)
	}

	var contacts []any
	for i, c := range demoContacts {
		contact := models.Contact{
			ID: bson.NewObjectID(), Name: c[0], Email: c[1], Service: c[2], Status: c[4], Message: c[5],
			Read: c[4] != "new", CreatedAt: day(c[3]),
		}
		if c[4] == "flagged" {
			contact.FlagReason = "Bulk spam, not a real enquiry."
			if i == 8 {
				contact.FlagReason = "Address bounces — nothing to reply to."
			}
		}
		contacts = append(contacts, contact)
	}

	for _, batch := range []struct {
		coll *mongo.Collection
		docs []any
	}{{d.Clients, clients}, {d.Jobs, jobs}, {d.Trials, trials}, {d.Contacts, contacts}} {
		if _, err := batch.coll.InsertMany(ctx, batch.docs); err != nil {
			return fmt.Errorf("demo %s: %w", batch.coll.Name(), err)
		}
		slog.Info("demo data loaded", "collection", batch.coll.Name(), "documents", len(batch.docs))
	}
	return nil
}
