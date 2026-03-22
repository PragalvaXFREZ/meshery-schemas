package core

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"github.com/gofrs/uuid"
)

func ResolvedAliasFromNonResolved(nonResolved NonResolvedAlias, resolvedParentId uuid.UUID, resolvedField []string) ResolvedAlias {

	return ResolvedAlias{
		AliasComponentId:      nonResolved.AliasComponentId,
		ImmediateParentId:     nonResolved.ImmediateParentId,
		ImmediateRefFieldPath: nonResolved.ImmediateRefFieldPath,
		RelationshipId:        nonResolved.RelationshipId,
		ResolvedParentId:      resolvedParentId,
		ResolvedRefFieldPath:  resolvedField,
	}
}

// GormDataType returns the data type for GORM to use when creating the database column.
func (m MapObject) GormDataType() string {
	return "text"
}

// Scan implements the sql.Scanner interface for MapObject.
func (m *MapObject) Scan(src interface{}) error {
	var b []byte
	switch t := src.(type) {
	case nil:
		*m = nil
		return nil
	case []byte:
		b = t
	case string:
		b = []byte(t)
	default:
		return fmt.Errorf("scan source was not []byte nor string but %T", src)
	}
	return json.Unmarshal(b, m)
}

// Value implements the driver.Valuer interface for MapObject.
func (m MapObject) Value() (driver.Value, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}
