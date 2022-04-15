/*
Copyright © 2022 bill <mengseeker@yeah.net>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/
package cmd

import (
	"fmt"
	"ginapp/cmd/migrate"
	"ginapp/models"

	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

//go:generate stringer -type Migration -linecomment
type Migration int
type MigrateAction func(db *gorm.DB) error

const (
	// 执行顺序从后到前
	MAuto Migration = iota
	MUser1

	MCount
)

var (
	Migrates [MCount]bool

	// registry migrations
	migrations = map[Migration]MigrateAction{
		MAuto:  migrate.AutoMigrate,
		MUser1: migrate.User1,
	}
)

// migrateCmd represents the migrate command
var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "run migrate",
	Long:  `.`,
	Run: func(cmd *cobra.Command, args []string) {
		var err error
		InitDB()
		if err = Migrate(); err != nil {
			panic(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(migrateCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// migrateCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// migrateCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	migrateCmd.Flags().BoolVar(&Migrates[MAuto], "auto", true, "do AutoMigrate")
	migrateCmd.Flags().BoolVar(&Migrates[MUser1], "muser1", false, "remove field age for table uses")
}

func Migrate() error {
	var err error
	migdb := models.DB.Debug()
	for i := MCount - 1; i >= 0; i-- {
		if Migrates[i] {
			if m, ok := migrations[i]; ok {
				if err = m(migdb); err != nil {
					return fmt.Errorf("%s: %v", i, err)
				}
			}
		}
	}
	return nil
}
