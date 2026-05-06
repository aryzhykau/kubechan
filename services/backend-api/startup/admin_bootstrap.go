// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package startup

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"golang.org/x/crypto/bcrypt"
)

const adminSecretName = "kubechan-admin-credentials"
const adminUsername = "admin"

// EnsureAdminUser checks whether an admin user exists in the DB.
// If not, it reads (or creates) the kubechan-admin-credentials K8s Secret to
// obtain the password, then inserts the admin user.
func EnsureAdminUser(ctx context.Context, db *sql.DB, k8s client.Client, namespace string, logger *slog.Logger) error {
	// 1. Check if any admin user exists.
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&count); err != nil {
		return fmt.Errorf("checking admin user: %w", err)
	}
	if count > 0 {
		logger.Info("admin user already exists, skipping bootstrap")
		return nil
	}

	// 2. Resolve password from K8s Secret.
	password, err := resolveAdminPassword(ctx, k8s, namespace, logger)
	if err != nil {
		return fmt.Errorf("resolving admin password: %w", err)
	}

	// 3. Hash and insert admin user.
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return fmt.Errorf("hashing admin password: %w", err)
	}

	id := uuid.New().String()
	_, err = db.ExecContext(ctx,
		`INSERT INTO users (id, username, password_hash, role) VALUES (?, ?, ?, 'admin')`,
		id, adminUsername, string(hash),
	)
	if err != nil {
		return fmt.Errorf("inserting admin user: %w", err)
	}

	logger.Info("admin user created", "username", adminUsername, "id", id)
	return nil
}

// resolveAdminPassword returns the admin password.
// If the Secret exists, it reads the "password" key.
// If not, it generates a random password and creates the Secret.
func resolveAdminPassword(ctx context.Context, k8s client.Client, namespace string, logger *slog.Logger) (string, error) {
	secret := &corev1.Secret{}
	key := client.ObjectKey{Namespace: namespace, Name: adminSecretName}

	err := k8s.Get(ctx, key, secret)
	if err == nil {
		// Secret exists — use the stored password.
		raw, ok := secret.Data["password"]
		if !ok || len(raw) == 0 {
			return "", fmt.Errorf("secret %s/%s exists but has no 'password' key", namespace, adminSecretName)
		}
		logger.Info("using existing admin credentials secret", "secret", adminSecretName)
		return string(raw), nil
	}

	if !apierrors.IsNotFound(err) {
		return "", fmt.Errorf("getting secret %s: %w", adminSecretName, err)
	}

	// Secret does not exist — generate a random password and create the Secret.
	password, err := generatePassword(24)
	if err != nil {
		return "", fmt.Errorf("generating random password: %w", err)
	}

	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      adminSecretName,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"password": []byte(password),
		},
	}
	if err := k8s.Create(ctx, newSecret); err != nil {
		return "", fmt.Errorf("creating admin credentials secret: %w", err)
	}

	logger.Info("created admin credentials secret", "secret", adminSecretName, "namespace", namespace)
	return password, nil
}

// generatePassword returns a URL-safe base64-encoded random string of at least n characters.
func generatePassword(n int) (string, error) {
	// Each base64 char encodes 6 bits; read enough bytes.
	b := make([]byte, (n*6+7)/8+4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s := base64.URLEncoding.EncodeToString(b)
	return s[:n], nil
}
