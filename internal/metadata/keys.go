package metadata

import "database/sql"

// AccessKey represents an API access key.
type AccessKey struct {
	AccessKeyID string
	SecretKey   string
	DisplayName string
	IsRoot      bool
}

// CreateAccessKey creates a new access key.
func (s *Store) CreateAccessKey(accessKeyID, secretKey, displayName string, isRoot bool) error {
	_, err := s.db.Exec(`
		INSERT INTO trove_access_keys (access_key_id, secret_key, display_name, is_root)
		VALUES ($1, $2, $3, $4)
	`, accessKeyID, secretKey, displayName, isRoot)
	return err
}

// GetAccessKey retrieves an access key by ID.
func (s *Store) GetAccessKey(accessKeyID string) (*AccessKey, error) {
	key := &AccessKey{AccessKeyID: accessKeyID}
	err := s.db.QueryRow(`
		SELECT secret_key, display_name, is_root
		FROM trove_access_keys WHERE access_key_id = $1
	`, accessKeyID).Scan(&key.SecretKey, &key.DisplayName, &key.IsRoot)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return key, err
}

// ListRootKeys returns all root access keys.
func (s *Store) ListRootKeys() ([]AccessKey, error) {
	rows, err := s.db.Query(`
		SELECT access_key_id, secret_key, display_name, is_root
		FROM trove_access_keys WHERE is_root = true
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []AccessKey
	for rows.Next() {
		var k AccessKey
		if err := rows.Scan(&k.AccessKeyID, &k.SecretKey, &k.DisplayName, &k.IsRoot); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// DeleteAccessKey removes an access key and its permissions.
func (s *Store) DeleteAccessKey(accessKeyID string) error {
	_, err := s.db.Exec(`DELETE FROM trove_access_keys WHERE access_key_id = $1`, accessKeyID)
	return err
}

// GrantBucketPermission grants permissions on a bucket to an access key.
func (s *Store) GrantBucketPermission(accessKeyID, bucket string, canRead, canWrite, isOwner bool) error {
	_, err := s.db.Exec(`
		INSERT INTO trove_bucket_permissions (access_key_id, bucket, can_read, can_write, is_owner)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (access_key_id, bucket) DO UPDATE SET
			can_read = EXCLUDED.can_read,
			can_write = EXCLUDED.can_write,
			is_owner = EXCLUDED.is_owner
	`, accessKeyID, bucket, canRead, canWrite, isOwner)
	return err
}

// CheckPermission checks if an access key has the required permission on a bucket.
// Root keys have all permissions on all buckets.
func (s *Store) CheckPermission(accessKeyID, bucket string, needRead, needWrite bool) (bool, error) {
	// Check if root
	key, err := s.GetAccessKey(accessKeyID)
	if err != nil {
		return false, err
	}
	if key == nil {
		return false, nil
	}
	if key.IsRoot {
		return true, nil
	}

	var canRead, canWrite bool
	err = s.db.QueryRow(`
		SELECT can_read, can_write
		FROM trove_bucket_permissions
		WHERE access_key_id = $1 AND bucket = $2
	`, accessKeyID, bucket).Scan(&canRead, &canWrite)

	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if needRead && !canRead {
		return false, nil
	}
	if needWrite && !canWrite {
		return false, nil
	}

	return true, nil
}
