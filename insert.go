/*
Package gormcreatewoi provides a insert method using a DB instance of gorm without add the OUTPUT Inserted clausule.
*/
package gormcreatewoi

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/jinzhu/gorm"
)

// BulkInsert executes the query to insert multiple records at once.
//
// [objects] must be a slice of struct.
//
// [chunkSize] is a number of variables embedded in query. To prevent the error which occurs embedding a large number of variables at once
// and exceeds the limit of prepared statement. Larger size normally leads to better performance, in most cases 2000 to 3000 is reasonable.
//
// [excludeColumns] is column names to exclude from insert.
func InsertWOI(db *gorm.DB, objects interface{}, excludeColumns ...string) error {
	// Split records with specified size not to exceed Database parameter limit
	if err := insertObjSet(db, objects, excludeColumns...); err != nil {
		return err
	}
	return nil
}

func insertObjSet(db *gorm.DB, objects interface{}, excludeColumns ...string) error {
	if objects == nil {
		return nil
	}

	firstAttrs, err := extractMapValue(objects, excludeColumns)
	if err != nil {
		return err
	}

	attrSize := len(firstAttrs)

	// Scope to eventually run SQL
	mainScope := db.NewScope(objects)
	// Store placeholders for embedding variables
	placeholders := make([]string, 0, attrSize)

	// Replace with database column name
	dbColumns := make([]string, 0, attrSize)
	for _, key := range sortedKeys(firstAttrs) {
		dbColumns = append(dbColumns, mainScope.Quote(key))
	}

	//for _, obj := range objects {
	objAttrs, err := extractMapValue(objects, excludeColumns)
	if err != nil {
		return err
	}

	// If object sizes are different, SQL statement loses consistency
	if len(objAttrs) != attrSize {
		return errors.New("attribute sizes are inconsistent")
	}

	scope := db.NewScope(objects)

	// Append variables
	variables := make([]string, 0, attrSize)
	for _, key := range sortedKeys(objAttrs) {
		scope.AddToVars(objAttrs[key])
		variables = append(variables, "?")
	}

	valueQuery := "(" + strings.Join(variables, ", ") + ")"
	placeholders = append(placeholders, valueQuery)

	// Also append variables to mainScope
	mainScope.SQLVars = append(mainScope.SQLVars, scope.SQLVars...)
	//}

	insertOption := ""
	if val, ok := db.Get("gorm:insert_option"); ok {
		strVal, ok := val.(string)
		if !ok {
			return errors.New("gorm:insert_option should be a string")
		}
		insertOption = strVal
	}

	mainScope.Raw(fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES %s %s",
		mainScope.QuotedTableName(),
		strings.Join(dbColumns, ", "),
		strings.Join(placeholders, ", "),
		insertOption,
	))

	return db.Debug().Exec(mainScope.SQL, mainScope.SQLVars...).Error
}

// Obtain columns and values required for insert from interface
func extractMapValue(value interface{}, excludeColumns []string) (map[string]interface{}, error) {
	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
		value = rv.Interface()
	}
	if rv.Kind() != reflect.Struct {
		return nil, errors.New("value must be kind of Struct")
	}

	var attrs = map[string]interface{}{}

	for _, field := range (&gorm.Scope{Value: value}).Fields() {
		// Exclude relational record because it's not directly contained in database columns
		_, hasForeignKey := field.TagSettingsGet("FOREIGNKEY")

		if !containString(excludeColumns, field.Struct.Name) && field.StructField.Relationship == nil && !hasForeignKey &&
			!field.IsIgnored && !fieldIsAutoIncrement(field) && !fieldIsPrimaryAndBlank(field) {
			if (field.Struct.Name == "CreatedAt" || field.Struct.Name == "UpdatedAt") && field.IsBlank {
				attrs[field.DBName] = time.Now()
			} else if field.StructField.HasDefaultValue && field.IsBlank {
				// If default value presents and field is empty, assign a default value
				if val, ok := field.TagSettingsGet("DEFAULT"); ok {
					attrs[field.DBName] = val
				} else {
					attrs[field.DBName] = field.Field.Interface()
				}
			} else {
				attrs[field.DBName] = field.Field.Interface()
			}
		}
	}
	return attrs, nil
}

func fieldIsAutoIncrement(field *gorm.Field) bool {
	if value, ok := field.TagSettingsGet("AUTO_INCREMENT"); ok {
		return strings.ToLower(value) != "false"
	}
	return false
}

func fieldIsPrimaryAndBlank(field *gorm.Field) bool {
	return field.IsPrimaryKey && field.IsBlank
}
