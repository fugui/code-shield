package models

import (
	"log"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// RunDataMigration checks if old tables exist and migrates/merges data into the new schema.
func RunDataMigration(db *gorm.DB) {
	// Check if either the old "members" table or its backup exists.
	if !db.Migrator().HasTable("members") && !db.Migrator().HasTable("_old_members_backup") {
		return
	}

	log.Println("DB Migration: Detected old schema tables. Checking data migration...")

	// 1. Rename old tables to backups
	if db.Migrator().HasTable("members") {
		if err := db.Migrator().RenameTable("members", "_old_members_backup"); err != nil {
			log.Fatalf("DB Migration Failed: failed to rename 'members' table: %v", err)
		}
	}
	if db.Migrator().HasTable("teams") {
		if err := db.Migrator().RenameTable("teams", "_old_teams_backup"); err != nil {
			log.Fatalf("DB Migration Failed: failed to rename 'teams' table: %v", err)
		}
	}

	// 2. Perform GORM AutoMigrate to create the new tables (departments, updated users, etc.)
	log.Println("DB Migration: Running AutoMigrate to establish new schema...")
	err := db.AutoMigrate(
		&User{},
		&Department{},
		&Repository{},
		&TaskType{},
		&TaskReport{},
		&KeyIssue{},
		&SystemConfig{},
		&ScheduleConfig{},
		&TaskExecutionLog{},
		&TestCaseFinding{},
		&CoredumpFinding{},
		&FloatFinding{},
		&ThreadFinding{},
		&CjsonFinding{},
		&AnalysisFinding{},
	)
	if err != nil {
		log.Fatalf("DB Migration Failed: failed to run AutoMigrate: %v", err)
	}

	// 3. Migrate and copy Team data to Department
	log.Println("DB Migration: Migrating Team records to Department...")
	type OldTeam struct {
		ID        uint
		Name      string
		LeaderID  string
		CreatedAt string
	}
	var oldTeams []OldTeam
	if db.Migrator().HasTable("_old_teams_backup") {
		if err := db.Raw("SELECT id, name, leader_id, created_at FROM _old_teams_backup").Scan(&oldTeams).Error; err != nil {
			log.Printf("DB Migration Warning: failed to read old teams: %v", err)
		}
	}

	for _, ot := range oldTeams {
		// Insert department with matching ID to preserve relation references
		dept := Department{
			ID:   ot.ID,
			Name: ot.Name,
		}
		if err := db.Create(&dept).Error; err != nil {
			// Ignore key conflict in subsequent runs
		}
	}

	// 4. Migrate and merge Member and User data
	log.Println("DB Migration: Merging Member and User data based on Email...")
	type OldMember struct {
		ID         string
		Name       string
		Email      string
		Department string
	}
	var oldMembers []OldMember
	if db.Migrator().HasTable("_old_members_backup") {
		if err := db.Raw("SELECT id, name, email, department FROM _old_members_backup").Scan(&oldMembers).Error; err != nil {
			log.Printf("DB Migration Warning: failed to read old members: %v", err)
		}
	}

	// Read existing users to check matches
	var existingUsers []User
	if err := db.Find(&existingUsers).Error; err != nil {
		log.Printf("DB Migration Warning: failed to read existing users: %v", err)
	}

	userEmailMap := make(map[string]*User)
	for i := range existingUsers {
		userEmailMap[existingUsers[i].Email] = &existingUsers[i]
	}

	// Map old member ID (string) to new User.ID (uint)
	memberIDMap := make(map[string]uint)

	// Placeholder password for imported users
	invalidPassword, _ := bcrypt.GenerateFromPassword([]byte("imported-account-no-local-password"), bcrypt.DefaultCost)

	for _, m := range oldMembers {
		// Determine department ID from name
		var deptID *uint
		if m.Department != "" {
			var dept Department
			if err := db.Where("name = ?", m.Department).First(&dept).Error; err == nil {
				deptID = &dept.ID
			}
		}

		if u, ok := userEmailMap[m.Email]; ok {
			// Situation A: User with this email already exists (SSO or local)
			// Merge member info into User
			u.EmployeeID = m.ID
			if u.Name == "" {
				u.Name = m.Name
			}
			u.DepartmentID = deptID
			if err := db.Save(u).Error; err != nil {
				log.Printf("DB Migration Warning: failed to merge member %s to user: %v", m.Email, err)
			}
			memberIDMap[m.ID] = u.ID
		} else {
			// Situation B: Only exists in Member
			// Create a new inactive user
			newUser := User{
				EmployeeID:   m.ID,
				Name:         m.Name,
				Email:        m.Email,
				Password:     string(invalidPassword),
				RegMethod:    "imported",
				IsActive:     false,
				DepartmentID: deptID,
			}
			if err := db.Create(&newUser).Error; err != nil {
				log.Printf("DB Migration Warning: failed to create imported user for member %s: %v", m.Email, err)
			} else {
				memberIDMap[m.ID] = newUser.ID
			}
		}
	}

	// 5. Back-fill Department Leaders
	log.Println("DB Migration: Back-filling Department Leader IDs...")
	for _, ot := range oldTeams {
		trimmedLeaderID := strings.TrimSpace(ot.LeaderID)
		if trimmedLeaderID != "" {
			if newLeaderID, ok := memberIDMap[trimmedLeaderID]; ok {
				if err := db.Model(&Department{}).Where("id = ?", ot.ID).Update("leader_id", newLeaderID).Error; err != nil {
					log.Printf("DB Migration Warning: failed to update department %d leader: %v", ot.ID, err)
				}
			}
		}
	}

	// 6. Cascade update Repository Owner IDs and Department IDs
	log.Println("DB Migration: Cascading updates to Repository relations...")
	type OldRepo struct {
		ID           uint
		TeamID       uint
		DepartmentID uint
		OwnerID      string
	}
	var oldRepos []OldRepo
	if err := db.Raw("SELECT id, team_id, department_id, owner_id FROM repositories").Scan(&oldRepos).Error; err == nil {
		for _, or := range oldRepos {
			trimmedOwnerID := strings.TrimSpace(or.OwnerID)
			_, numErr := strconv.Atoi(trimmedOwnerID)
			isAlreadyNumeric := numErr == nil && trimmedOwnerID != ""

			var targetDeptID uint
			if or.TeamID > 0 {
				targetDeptID = or.TeamID
			} else {
				targetDeptID = or.DepartmentID
			}

			if isAlreadyNumeric {
				// Already migrated to numeric user ID, just ensure department_id is set
				if err := db.Exec("UPDATE repositories SET department_id = ? WHERE id = ?", targetDeptID, or.ID).Error; err != nil {
					log.Printf("DB Migration Warning: failed to update repository %d department: %v", or.ID, err)
				}
			} else {
				// Still an old string ID (like "白宇" or "fugui 08163")
				newOwnerID, ok := memberIDMap[trimmedOwnerID]
				if ok {
					if err := db.Exec("UPDATE repositories SET department_id = ?, owner_id = ? WHERE id = ?", targetDeptID, newOwnerID, or.ID).Error; err != nil {
						log.Printf("DB Migration Warning: failed to update repository %d: %v", or.ID, err)
					}
					log.Printf("DB Migration: Updated repository %d owner from %q to %d", or.ID, trimmedOwnerID, newOwnerID)
				} else {
					// Fallback to 0 if owner not found in members table
					if err := db.Exec("UPDATE repositories SET department_id = ?, owner_id = 0 WHERE id = ?", targetDeptID, or.ID).Error; err != nil {
						log.Printf("DB Migration Warning: failed to update repository %d to default owner: %v", or.ID, err)
					}
					log.Printf("DB Migration: Updated repository %d owner from %q to default (0)", or.ID, trimmedOwnerID)
				}
			}
		}
	}

	// 7. Cascade update Findings tables
	findingsTables := []string{
		"test_case_findings",
		"coredump_findings",
		"float_findings",
		"thread_findings",
		"cjson_findings",
		"analysis_findings",
		"key_issues",
	}

	log.Println("DB Migration: Cascading updates to Findings Assignee IDs...")
	for _, table := range findingsTables {
		type OldFinding struct {
			ID         uint
			AssigneeID string
		}
		var oldFindings []OldFinding
		if err := db.Raw("SELECT id, assignee_id FROM " + table).Scan(&oldFindings).Error; err == nil {
			for _, f := range oldFindings {
				trimmedAssigneeID := strings.TrimSpace(f.AssigneeID)
				if trimmedAssigneeID != "" {
					// Check if already numeric
					_, err := strconv.Atoi(trimmedAssigneeID)
					if err == nil {
						// Already migrated, keep it
						continue
					}

					// Still an old string ID
					if newAssigneeID, ok := memberIDMap[trimmedAssigneeID]; ok {
						if err := db.Exec("UPDATE "+table+" SET assignee_id = ? WHERE id = ?", newAssigneeID, f.ID).Error; err != nil {
							log.Printf("DB Migration Warning: failed to update table %s finding %d: %v", table, f.ID, err)
						}
					} else {
						// Set to NULL if no match
						if err := db.Exec("UPDATE "+table+" SET assignee_id = NULL WHERE id = ?", f.ID).Error; err != nil {
							log.Printf("DB Migration Warning: failed to set NULL for table %s finding %d: %v", table, f.ID, err)
						}
					}
				}
			}
		}
	}

	log.Println("DB Migration: Successfully completed all steps.")
}
