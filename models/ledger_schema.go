package models

import (
	"fmt"

	"gorm.io/gorm"
)

type ledgerForeignKey struct {
	table      string
	constraint string
	sql        string
}

func ensureLedgerForeignKeys(db *gorm.DB) error {
	orphans := []struct {
		name string
		sql  string
	}{
		{
			name: "defect_aliases.defect_id",
			sql: `SELECT COUNT(*) FROM defect_aliases AS child
				LEFT JOIN defects AS parent ON parent.id = child.defect_id
				WHERE parent.id IS NULL`,
		},
		{
			name: "defect_observations.defect_id",
			sql: `SELECT COUNT(*) FROM defect_observations AS child
				LEFT JOIN defects AS parent ON parent.id = child.defect_id
				WHERE child.defect_id IS NOT NULL AND parent.id IS NULL`,
		},
		{
			name: "defect_events.defect_id",
			sql: `SELECT COUNT(*) FROM defect_events AS child
				LEFT JOIN defects AS parent ON parent.id = child.defect_id
				WHERE parent.id IS NULL`,
		},
		{
			name: "defects.merged_into_id",
			sql: `SELECT COUNT(*) FROM defects AS child
				LEFT JOIN defects AS parent ON parent.id = child.merged_into_id
				WHERE child.merged_into_id IS NOT NULL AND parent.id IS NULL`,
		},
	}
	for _, item := range orphans {
		var count int64
		if err := db.Raw(item.sql).Scan(&count).Error; err != nil {
			return fmt.Errorf("check orphan rows for %s: %w", item.name, err)
		}
		if count > 0 {
			return fmt.Errorf("%s has %d orphan rows; foreign key creation skipped", item.name, count)
		}
	}

	keys := []ledgerForeignKey{
		{
			table: "defect_aliases", constraint: "fk_defect_aliases_defect",
			sql: "ALTER TABLE defect_aliases ADD CONSTRAINT fk_defect_aliases_defect FOREIGN KEY (defect_id) REFERENCES defects(id) ON DELETE CASCADE",
		},
		{
			table: "defect_observations", constraint: "fk_defect_observations_defect",
			sql: "ALTER TABLE defect_observations ADD CONSTRAINT fk_defect_observations_defect FOREIGN KEY (defect_id) REFERENCES defects(id) ON DELETE SET NULL",
		},
		{
			table: "defect_events", constraint: "fk_defect_events_defect",
			sql: "ALTER TABLE defect_events ADD CONSTRAINT fk_defect_events_defect FOREIGN KEY (defect_id) REFERENCES defects(id) ON DELETE CASCADE",
		},
		{
			table: "defects", constraint: "fk_defects_merged_into",
			sql: "ALTER TABLE defects ADD CONSTRAINT fk_defects_merged_into FOREIGN KEY (merged_into_id) REFERENCES defects(id) ON DELETE SET NULL",
		},
	}
	for _, key := range keys {
		exists, err := foreignKeyExists(db, key.table, key.constraint)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if err := db.Exec(key.sql).Error; err != nil {
			return fmt.Errorf("create %s: %w", key.constraint, err)
		}
	}
	return nil
}

func foreignKeyExists(db *gorm.DB, table, constraint string) (bool, error) {
	var count int64
	err := db.Table("information_schema.table_constraints").
		Where("constraint_type = ? AND table_schema = current_schema() AND table_name = ? AND constraint_name = ?",
			"FOREIGN KEY", table, constraint).
		Count(&count).Error
	return count > 0, err
}
