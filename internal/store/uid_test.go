package store

import (
	"path/filepath"
	"testing"

	"github.com/OOPQ/reelkeep/internal/model"
)

func TestRelationsFollowCurrentTokenByUID(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	row := model.Row{Token: "demo@42", Source: "demo", SourceID: "42", Status: "new", Title: "A"}
	if err := st.UpsertRows([]model.Row{row}); err != nil {
		t.Fatal(err)
	}
	cid, err := st.CreateCollection("c", "")
	if err != nil {
		t.Fatal(err)
	}
	tid, err := st.CreateTag("t", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddToCollection(cid, []string{"demo@42"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddToTag(tid, []string{"demo@42"}); err != nil {
		t.Fatal(err)
	}

	// Simulate a token migration that updated items but left relation rows on
	// the old token. UID joins must still resolve to the current token.
	if _, err := st.db.Exec("UPDATE items SET token = 'demo@renamed' WHERE token = 'demo@42'"); err != nil {
		t.Fatal(err)
	}
	tokens, err := st.CollectionTokens(cid)
	if err != nil || len(tokens) != 1 || tokens[0] != "demo@renamed" {
		t.Fatalf("collection tokens = %v, err=%v", tokens, err)
	}
	per, err := st.CollectionsForToken("demo@renamed")
	if err != nil || !per[cid] {
		t.Fatalf("collections for token = %v, err=%v", per, err)
	}
	tags, err := st.TagsForToken("demo@renamed")
	if err != nil || !tags[tid] {
		t.Fatalf("tags for token = %v, err=%v", tags, err)
	}
	tagTokens, err := st.TagTokens(tid)
	if err != nil || len(tagTokens) != 1 || tagTokens[0] != "demo@renamed" {
		t.Fatalf("tag tokens = %v, err=%v", tagTokens, err)
	}
	byToken, err := st.TagsByToken([]string{"demo@renamed"})
	if err != nil || len(byToken["demo@renamed"]) != 1 {
		t.Fatalf("tags by token = %v, err=%v", byToken, err)
	}
}
