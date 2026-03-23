package storage

import (
	"classact/internal/model"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// Feature: class-action-scraper, Property 4: Upsert idempotency
// **Validates: Requirements 2.3, 8.3**
func TestProperty4_UpsertIdempotency(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		repo, err := NewSQLiteRepository(":memory:")
		if err != nil {
			rt.Fatalf("NewSQLiteRepository: %v", err)
		}
		defer repo.Close()
		ctx := context.Background()

		// Generate 1-10 unique source URLs, each with a random record
		numRecords := rapid.IntRange(1, 10).Draw(rt, "numRecords")

		type recordEntry struct {
			sourceURL   string
			lastRecord  model.LawsuitRecord
			upsertCount int
		}

		entries := make([]recordEntry, numRecords)
		for i := 0; i < numRecords; i++ {
			sourceURL := fmt.Sprintf("https://example.com/lawsuit/%d/%s",
				i, rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, fmt.Sprintf("urlSuffix_%d", i)))
			upsertCount := rapid.IntRange(1, 5).Draw(rt, fmt.Sprintf("upsertCount_%d", i))
			entries[i] = recordEntry{
				sourceURL:   sourceURL,
				upsertCount: upsertCount,
			}
		}

		// For each record, upsert N times with potentially different field values each time
		for i := range entries {
			for j := 0; j < entries[i].upsertCount; j++ {
				rec := model.LawsuitRecord{
					ID:          fmt.Sprintf("id-%d-%d", i, j),
					Title:       rapid.StringMatching(`[A-Za-z ]{5,30}`).Draw(rt, fmt.Sprintf("title_%d_%d", i, j)),
					Description: rapid.StringMatching(`[A-Za-z ]{10,50}`).Draw(rt, fmt.Sprintf("desc_%d_%d", i, j)),
					SourceURL:   entries[i].sourceURL,
					Company:     rapid.StringMatching(`[A-Za-z ]{3,20}`).Draw(rt, fmt.Sprintf("company_%d_%d", i, j)),
					Status:      rapid.SampledFrom([]string{"open", "closed", "settled"}).Draw(rt, fmt.Sprintf("status_%d_%d", i, j)),
				}

				// Optionally set filing date
				if rapid.Bool().Draw(rt, fmt.Sprintf("hasFilingDate_%d_%d", i, j)) {
					fd := time.Date(
						rapid.IntRange(2020, 2025).Draw(rt, fmt.Sprintf("fdYear_%d_%d", i, j)),
						time.Month(rapid.IntRange(1, 12).Draw(rt, fmt.Sprintf("fdMonth_%d_%d", i, j))),
						rapid.IntRange(1, 28).Draw(rt, fmt.Sprintf("fdDay_%d_%d", i, j)),
						0, 0, 0, 0, time.UTC,
					)
					rec.FilingDate = &fd
				}

				// Optionally set deadline
				if rapid.Bool().Draw(rt, fmt.Sprintf("hasDeadline_%d_%d", i, j)) {
					dl := time.Date(
						rapid.IntRange(2025, 2030).Draw(rt, fmt.Sprintf("dlYear_%d_%d", i, j)),
						time.Month(rapid.IntRange(1, 12).Draw(rt, fmt.Sprintf("dlMonth_%d_%d", i, j))),
						rapid.IntRange(1, 28).Draw(rt, fmt.Sprintf("dlDay_%d_%d", i, j)),
						0, 0, 0, 0, time.UTC,
					)
					rec.Deadline = &dl
				}

				_, _, err := repo.UpsertLawsuits(ctx, []model.LawsuitRecord{rec})
				if err != nil {
					rt.Fatalf("UpsertLawsuits failed on record %d upsert %d: %v", i, j, err)
				}

				// Track the last upserted values for this source URL
				entries[i].lastRecord = rec
			}
		}

		// Verify: exactly one record per unique source URL
		all, err := repo.ListLawsuits(ctx, LawsuitFilter{})
		if err != nil {
			rt.Fatalf("ListLawsuits: %v", err)
		}

		if len(all) != numRecords {
			rt.Fatalf("expected %d records, got %d", numRecords, len(all))
		}

		// Build a lookup by source URL for verification
		byURL := make(map[string]model.LawsuitRecord, len(all))
		for _, rec := range all {
			if _, exists := byURL[rec.SourceURL]; exists {
				rt.Fatalf("duplicate source URL in results: %s", rec.SourceURL)
			}
			byURL[rec.SourceURL] = rec
		}

		// Verify each record's fields match the most recent upsert
		for _, entry := range entries {
			got, ok := byURL[entry.sourceURL]
			if !ok {
				rt.Fatalf("missing record for source URL: %s", entry.sourceURL)
			}

			last := entry.lastRecord

			if got.Title != last.Title {
				rt.Errorf("source_url=%s: title = %q, want %q", entry.sourceURL, got.Title, last.Title)
			}
			if got.Description != last.Description {
				rt.Errorf("source_url=%s: description = %q, want %q", entry.sourceURL, got.Description, last.Description)
			}
			if got.Company != last.Company {
				rt.Errorf("source_url=%s: company = %q, want %q", entry.sourceURL, got.Company, last.Company)
			}
			if got.Status != last.Status {
				rt.Errorf("source_url=%s: status = %q, want %q", entry.sourceURL, got.Status, last.Status)
			}

			// Compare filing date
			if last.FilingDate == nil {
				if got.FilingDate != nil {
					rt.Errorf("source_url=%s: filing_date = %v, want nil", entry.sourceURL, got.FilingDate)
				}
			} else {
				if got.FilingDate == nil {
					rt.Errorf("source_url=%s: filing_date = nil, want %v", entry.sourceURL, last.FilingDate)
				} else if !got.FilingDate.Equal(*last.FilingDate) {
					rt.Errorf("source_url=%s: filing_date = %v, want %v", entry.sourceURL, got.FilingDate, last.FilingDate)
				}
			}

			// Compare deadline
			if last.Deadline == nil {
				if got.Deadline != nil {
					rt.Errorf("source_url=%s: deadline = %v, want nil", entry.sourceURL, got.Deadline)
				}
			} else {
				if got.Deadline == nil {
					rt.Errorf("source_url=%s: deadline = nil, want %v", entry.sourceURL, last.Deadline)
				} else if !got.Deadline.Equal(*last.Deadline) {
					rt.Errorf("source_url=%s: deadline = %v, want %v", entry.sourceURL, got.Deadline, last.Deadline)
				}
			}
		}
	})
}

// Feature: class-action-scraper, Property 10: Mark-as-applied round trip with idempotency
// **Validates: Requirements 5.1, 5.2, 5.3, 5.4, 11.4**
func TestProperty10_MarkAppliedRoundTripIdempotency(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		repo, err := NewSQLiteRepository(":memory:")
		if err != nil {
			rt.Fatalf("NewSQLiteRepository: %v", err)
		}
		defer repo.Close()
		ctx := context.Background()

		// Generate 1-10 random records
		numRecords := rapid.IntRange(1, 10).Draw(rt, "numRecords")

		type entry struct {
			record     model.LawsuitRecord
			markCount  int
			firstApply time.Time
		}
		entries := make([]entry, numRecords)

		// Insert all records first
		for i := 0; i < numRecords; i++ {
			rec := model.LawsuitRecord{
				ID:        fmt.Sprintf("prop10-id-%d-%s", i, rapid.StringMatching(`[a-z]{4,8}`).Draw(rt, fmt.Sprintf("idSuffix_%d", i))),
				Title:     rapid.StringMatching(`[A-Za-z ]{5,30}`).Draw(rt, fmt.Sprintf("title_%d", i)),
				Description: rapid.StringMatching(`[A-Za-z ]{10,50}`).Draw(rt, fmt.Sprintf("desc_%d", i)),
				SourceURL: fmt.Sprintf("https://example.com/prop10/%d/%s", i, rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, fmt.Sprintf("urlSuffix_%d", i))),
				Company:   rapid.StringMatching(`[A-Za-z]{3,15}`).Draw(rt, fmt.Sprintf("company_%d", i)),
				Status:    rapid.SampledFrom([]string{"open", "closed", "settled"}).Draw(rt, fmt.Sprintf("status_%d", i)),
			}

			if rapid.Bool().Draw(rt, fmt.Sprintf("hasFilingDate_%d", i)) {
				fd := time.Date(
					rapid.IntRange(2020, 2025).Draw(rt, fmt.Sprintf("fdYear_%d", i)),
					time.Month(rapid.IntRange(1, 12).Draw(rt, fmt.Sprintf("fdMonth_%d", i))),
					rapid.IntRange(1, 28).Draw(rt, fmt.Sprintf("fdDay_%d", i)),
					0, 0, 0, 0, time.UTC,
				)
				rec.FilingDate = &fd
			}

			if rapid.Bool().Draw(rt, fmt.Sprintf("hasDeadline_%d", i)) {
				dl := time.Date(
					rapid.IntRange(2025, 2030).Draw(rt, fmt.Sprintf("dlYear_%d", i)),
					time.Month(rapid.IntRange(1, 12).Draw(rt, fmt.Sprintf("dlMonth_%d", i))),
					rapid.IntRange(1, 28).Draw(rt, fmt.Sprintf("dlDay_%d", i)),
					0, 0, 0, 0, time.UTC,
				)
				rec.Deadline = &dl
			}

			_, _, err := repo.UpsertLawsuits(ctx, []model.LawsuitRecord{rec})
			if err != nil {
				rt.Fatalf("UpsertLawsuits record %d: %v", i, err)
			}

			entries[i] = entry{
				record:    rec,
				markCount: rapid.IntRange(1, 3).Draw(rt, fmt.Sprintf("markCount_%d", i)),
			}
		}

		// Verify none are applied before marking
		for i, e := range entries {
			applied, err := repo.GetAppliedStatus(ctx, e.record.ID)
			if err != nil {
				rt.Fatalf("GetAppliedStatus before mark (record %d): %v", i, err)
			}
			if applied {
				rt.Fatalf("record %d should not be applied before MarkApplied", i)
			}
		}

		// Mark each record as applied N times, verifying idempotency
		for i := range entries {
			for call := 0; call < entries[i].markCount; call++ {
				err := repo.MarkApplied(ctx, entries[i].record.ID)
				if err != nil {
					rt.Fatalf("MarkApplied record %d call %d: %v", i, call, err)
				}

				// After every call, Applied must be true
				applied, err := repo.GetAppliedStatus(ctx, entries[i].record.ID)
				if err != nil {
					rt.Fatalf("GetAppliedStatus record %d call %d: %v", i, call, err)
				}
				if !applied {
					rt.Fatalf("record %d should be applied after MarkApplied call %d", i, call)
				}

				// Retrieve the record to check AppliedAt
				all, err := repo.ListLawsuits(ctx, LawsuitFilter{})
				if err != nil {
					rt.Fatalf("ListLawsuits record %d call %d: %v", i, call, err)
				}
				var found *model.LawsuitRecord
				for idx := range all {
					if all[idx].ID == entries[i].record.ID {
						found = &all[idx]
						break
					}
				}
				if found == nil {
					rt.Fatalf("record %d not found in ListLawsuits after call %d", i, call)
				}

				// AppliedAt must be non-nil after first call
				if found.AppliedAt == nil {
					rt.Fatalf("record %d AppliedAt is nil after MarkApplied call %d", i, call)
				}

				if call == 0 {
					// Capture the timestamp from the first call
					entries[i].firstApply = *found.AppliedAt
				} else {
					// Subsequent calls must NOT change AppliedAt (idempotency)
					if !found.AppliedAt.Equal(entries[i].firstApply) {
						rt.Fatalf("record %d AppliedAt changed after call %d: first=%v, now=%v",
							i, call, entries[i].firstApply, *found.AppliedAt)
					}
				}

				// Applied flag must be true on the listed record
				if !found.Applied {
					rt.Fatalf("record %d Applied flag is false in ListLawsuits after call %d", i, call)
				}
			}
		}

		// Final verification: ListLawsuits reflects all records as applied
		finalList, err := repo.ListLawsuits(ctx, LawsuitFilter{})
		if err != nil {
			rt.Fatalf("final ListLawsuits: %v", err)
		}
		if len(finalList) != numRecords {
			rt.Fatalf("expected %d records, got %d", numRecords, len(finalList))
		}

		appliedByID := make(map[string]model.LawsuitRecord, len(finalList))
		for _, rec := range finalList {
			appliedByID[rec.ID] = rec
		}

		for i, e := range entries {
			got, ok := appliedByID[e.record.ID]
			if !ok {
				rt.Fatalf("record %d (ID=%s) missing from final list", i, e.record.ID)
			}
			if !got.Applied {
				rt.Fatalf("record %d Applied=false in final list", i)
			}
			if got.AppliedAt == nil {
				rt.Fatalf("record %d AppliedAt=nil in final list", i)
			}
			if !got.AppliedAt.Equal(e.firstApply) {
				rt.Fatalf("record %d AppliedAt mismatch in final list: expected=%v, got=%v",
					i, e.firstApply, *got.AppliedAt)
			}
		}
	})
}

// Feature: class-action-scraper, Property 11: Company filter round trip
// **Validates: Requirements 6.1, 6.4, 6.5**
func TestProperty11_CompanyFilterRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		repo, err := NewSQLiteRepository(":memory:")
		if err != nil {
			rt.Fatalf("NewSQLiteRepository: %v", err)
		}
		defer repo.Close()
		ctx := context.Background()

		// Generate a pool of 1-15 company names with varying casing and whitespace
		poolSize := rapid.IntRange(1, 15).Draw(rt, "poolSize")
		namePool := make([]string, poolSize)
		for i := 0; i < poolSize; i++ {
			base := rapid.StringMatching(`[A-Za-z]{2,12}`).Draw(rt, fmt.Sprintf("baseName_%d", i))
			// Randomly add leading/trailing whitespace and vary casing
			prefix := rapid.StringMatching(`[ ]{0,3}`).Draw(rt, fmt.Sprintf("prefix_%d", i))
			suffix := rapid.StringMatching(`[ ]{0,3}`).Draw(rt, fmt.Sprintf("suffix_%d", i))
			namePool[i] = prefix + base + suffix
		}

		// Generate a random sequence of add/remove operations
		numOps := rapid.IntRange(1, 30).Draw(rt, "numOps")

		// Track expected state: set of normalized (lowercase, trimmed) names
		expected := make(map[string]bool)

		for op := 0; op < numOps; op++ {
			nameIdx := rapid.IntRange(0, poolSize-1).Draw(rt, fmt.Sprintf("nameIdx_%d", op))
			name := namePool[nameIdx]
			isAdd := rapid.Bool().Draw(rt, fmt.Sprintf("isAdd_%d", op))

			normalized := strings.ToLower(strings.TrimSpace(name))

			if isAdd {
				err := repo.AddCompany(ctx, name)
				if err != nil {
					rt.Fatalf("AddCompany(%q) op %d: %v", name, op, err)
				}
				expected[normalized] = true
			} else {
				err := repo.RemoveCompany(ctx, name)
				if err != nil {
					rt.Fatalf("RemoveCompany(%q) op %d: %v", name, op, err)
				}
				delete(expected, normalized)
			}
		}

		// Verify final state matches expected set
		got, err := repo.GetCompanyFilter(ctx)
		if err != nil {
			rt.Fatalf("GetCompanyFilter: %v", err)
		}

		gotSet := make(map[string]bool, len(got))
		for _, name := range got {
			if gotSet[name] {
				rt.Fatalf("duplicate name in GetCompanyFilter result: %q", name)
			}
			gotSet[name] = true
		}

		if len(gotSet) != len(expected) {
			rt.Fatalf("GetCompanyFilter returned %d names, expected %d\ngot: %v\nexpected: %v",
				len(gotSet), len(expected), gotSet, expected)
		}

		for name := range expected {
			if !gotSet[name] {
				rt.Fatalf("expected company %q not found in GetCompanyFilter result\ngot: %v", name, gotSet)
			}
		}

		for name := range gotSet {
			if !expected[name] {
				rt.Fatalf("unexpected company %q in GetCompanyFilter result\nexpected: %v", name, expected)
			}
		}
	})
}

// Feature: class-action-scraper, Property 12: Company filter matching
// **Validates: Requirements 6.2, 6.3, 11.3**
func TestProperty12_CompanyFilterMatching(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		repo, err := NewSQLiteRepository(":memory:")
		if err != nil {
			rt.Fatalf("NewSQLiteRepository: %v", err)
		}
		defer repo.Close()
		ctx := context.Background()

		// Generate a pool of company names with mixed casing
		companyPool := rapid.SliceOfN(
			rapid.Custom(func(rt *rapid.T) string {
				base := rapid.StringMatching(`[A-Za-z]{2,10}`).Draw(rt, "base")
				suffix := rapid.SampledFrom([]string{" Inc", " Corp", " LLC", " Ltd", ""}).Draw(rt, "suffix")
				return base + suffix
			}),
			2, 10,
		).Draw(rt, "companyPool")

		// Generate 1-15 records, each assigned a company from the pool
		numRecords := rapid.IntRange(1, 15).Draw(rt, "numRecords")
		records := make([]model.LawsuitRecord, numRecords)
		for i := 0; i < numRecords; i++ {
			company := companyPool[rapid.IntRange(0, len(companyPool)-1).Draw(rt, fmt.Sprintf("companyIdx_%d", i))]
			// Randomly vary casing of the company name
			casingChoice := rapid.IntRange(0, 2).Draw(rt, fmt.Sprintf("casing_%d", i))
			switch casingChoice {
			case 0:
				company = strings.ToUpper(company)
			case 1:
				company = strings.ToLower(company)
			// case 2: keep original mixed casing
			}

			records[i] = model.LawsuitRecord{
				ID:        fmt.Sprintf("p12-id-%d-%s", i, rapid.StringMatching(`[a-z]{4,8}`).Draw(rt, fmt.Sprintf("idSuffix_%d", i))),
				Title:     rapid.StringMatching(`[A-Za-z ]{5,20}`).Draw(rt, fmt.Sprintf("title_%d", i)),
				SourceURL: fmt.Sprintf("https://example.com/p12/%d/%s", i, rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, fmt.Sprintf("urlSuffix_%d", i))),
				Company:   company,
				Status:    rapid.SampledFrom([]string{"open", "closed", "settled"}).Draw(rt, fmt.Sprintf("status_%d", i)),
			}
		}

		// Insert all records
		_, _, err = repo.UpsertLawsuits(ctx, records)
		if err != nil {
			rt.Fatalf("UpsertLawsuits: %v", err)
		}

		// Generate a filter list: either empty (to test "return all") or a subset of company substrings
		useEmptyFilter := rapid.Bool().Draw(rt, "useEmptyFilter")

		var filterCompanies []string
		if !useEmptyFilter {
			// Pick 1-4 filter entries derived from the company pool (substrings or full names, mixed casing)
			numFilters := rapid.IntRange(1, 4).Draw(rt, "numFilters")
			for f := 0; f < numFilters; f++ {
				src := companyPool[rapid.IntRange(0, len(companyPool)-1).Draw(rt, fmt.Sprintf("filterSrcIdx_%d", f))]
				// Use a substring of the company name as the filter entry
				if len(src) > 2 {
					start := rapid.IntRange(0, len(src)/2).Draw(rt, fmt.Sprintf("subStart_%d", f))
					end := rapid.IntRange(start+1, len(src)).Draw(rt, fmt.Sprintf("subEnd_%d", f))
					src = src[start:end]
				}
				// Randomly vary casing of the filter entry
				fc := rapid.IntRange(0, 2).Draw(rt, fmt.Sprintf("filterCasing_%d", f))
				switch fc {
				case 0:
					src = strings.ToUpper(src)
				case 1:
					src = strings.ToLower(src)
				}
				filterCompanies = append(filterCompanies, src)
			}
		}

		// Call ListLawsuits with the filter
		result, err := repo.ListLawsuits(ctx, LawsuitFilter{Companies: filterCompanies})
		if err != nil {
			rt.Fatalf("ListLawsuits: %v", err)
		}

		if len(filterCompanies) == 0 {
			// Empty filter: all records should be returned
			if len(result) != numRecords {
				rt.Fatalf("empty filter: expected %d records, got %d", numRecords, len(result))
			}
		} else {
			// Non-empty filter: compute expected set using the same LIKE logic
			// A record matches if LOWER(company) LIKE '%' + LOWER(filterEntry) + '%' for any filter entry
			expectedIDs := make(map[string]bool)
			for _, rec := range records {
				lowerCompany := strings.ToLower(rec.Company)
				for _, f := range filterCompanies {
					if strings.Contains(lowerCompany, strings.ToLower(f)) {
						expectedIDs[rec.ID] = true
						break
					}
				}
			}

			resultIDs := make(map[string]bool, len(result))
			for _, rec := range result {
				resultIDs[rec.ID] = true
			}

			// Verify no extra records
			for id := range resultIDs {
				if !expectedIDs[id] {
					rt.Fatalf("unexpected record %s in filtered results (filter=%v)", id, filterCompanies)
				}
			}

			// Verify no missing records
			for id := range expectedIDs {
				if !resultIDs[id] {
					rt.Fatalf("missing record %s from filtered results (filter=%v)", id, filterCompanies)
				}
			}

			if len(result) != len(expectedIDs) {
				rt.Fatalf("filter result count mismatch: got %d, expected %d (filter=%v)",
					len(result), len(expectedIDs), filterCompanies)
			}
		}
	})
}
