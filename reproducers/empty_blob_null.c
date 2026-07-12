/*
 * Minimal CubeSQL C SDK reproducer for empty-BLOB versus SQL NULL cursor data.
 *
 * Credentials are read only from CUBESQL_* environment variables. The program
 * writes only to its dedicated sandbox database and verifies cleanup from a
 * newly authenticated connection.
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cubesql.h"

#define SANDBOX_DATABASE "go_cubesql_empty_blob_repro.db"
#define DEFAULT_HOST "localhost"
#define DEFAULT_PORT 4430
#define DEFAULT_TIMEOUT 12

struct config {
    const char *host;
    int port;
    const char *username;
    const char *password;
    int timeout;
};

static int environment_int(const char *name, int fallback) {
    const char *text = getenv(name);
    char *end = NULL;
    long value;

    if (text == NULL || text[0] == '\0') return fallback;
    value = strtol(text, &end, 10);
    if (end == text || *end != '\0' || value <= 0 || value > 65535) return -1;
    return (int)value;
}

static int load_config(struct config *config) {
    config->host = getenv("CUBESQL_HOST");
    if (config->host == NULL || config->host[0] == '\0') config->host = DEFAULT_HOST;
    config->port = environment_int("CUBESQL_PORT", DEFAULT_PORT);
    config->username = getenv("CUBESQL_USERNAME");
    config->password = getenv("CUBESQL_PASSWORD");
    config->timeout = environment_int("CUBESQL_TIMEOUT", DEFAULT_TIMEOUT);

    if (config->port < 0 || config->timeout < 0 || config->username == NULL ||
        config->username[0] == '\0' || config->password == NULL) {
        fprintf(stderr, "Missing or invalid CUBESQL connection environment.\n");
        return 0;
    }
    return 1;
}

static csqldb *connect_server(const struct config *config) {
    csqldb *db = NULL;
    int result = cubesql_connect(&db, config->host, config->port,
                                 config->username, config->password,
                                 config->timeout, CUBESQL_ENCRYPTION_NONE);
    if (result != CUBESQL_NOERR) {
        fprintf(stderr, "CubeSQL connection failed with code %d.\n", result);
        if (db != NULL) cubesql_disconnect(db, kFALSE);
        return NULL;
    }
    return db;
}

static int execute(csqldb *db, const char *sql, const char *operation) {
    if (cubesql_execute(db, sql) == CUBESQL_NOERR) return 1;
    fprintf(stderr, "%s failed with code %d: %s\n", operation,
            cubesql_errcode(db), cubesql_errmsg(db));
    return 0;
}

static int cleanup_database(const struct config *config) {
    csqldb *db = connect_server(config);
    int ok;

    if (db == NULL) return 0;
    ok = execute(db, "DROP DATABASE '" SANDBOX_DATABASE "' IF EXISTS;",
                 "sandbox cleanup");
    cubesql_disconnect(db, kTRUE);
    return ok;
}

static int verify_database_absent(const struct config *config) {
    csqldb *db = connect_server(config);
    int selected;

    if (db == NULL) return 0;
    selected = cubesql_set_database(db, SANDBOX_DATABASE);
    if (selected == CUBESQL_NOERR) {
        cubesql_set_database(db, NULL);
        execute(db, "DROP DATABASE '" SANDBOX_DATABASE "' IF EXISTS;",
                "verification recovery cleanup");
        cubesql_disconnect(db, kTRUE);
        return 0;
    }
    cubesql_disconnect(db, kTRUE);
    return 1;
}

static int field_equals(csqlc *cursor, int row, int column, const char *expected) {
    int length = 0;
    char *value = cubesql_cursor_field(cursor, row, column, &length);
    size_t expected_length = strlen(expected);

    return value != NULL && length == (int)expected_length &&
           memcmp(value, expected, expected_length) == 0;
}

static int inspect_cursor(csqlc *cursor) {
    int empty_length = 0;
    int null_length = 0;
    char *empty_value;
    char *null_value;
    int predicates_match;
    int bug_reproduced;

    if (cubesql_cursor_numrows(cursor) != 2 ||
        cubesql_cursor_numcolumns(cursor) != 5) {
        fprintf(stderr, "Unexpected cursor shape: rows=%d columns=%d.\n",
                cubesql_cursor_numrows(cursor),
                cubesql_cursor_numcolumns(cursor));
        return -1;
    }

    empty_value = cubesql_cursor_field(cursor, 1, 2, &empty_length);
    null_value = cubesql_cursor_field(cursor, 2, 2, &null_length);
    predicates_match =
        field_equals(cursor, 1, 1, "empty") &&
        field_equals(cursor, 1, 3, "0") &&
        field_equals(cursor, 1, 4, "blob") &&
        field_equals(cursor, 1, 5, "0") &&
        field_equals(cursor, 2, 1, "null") &&
        field_equals(cursor, 2, 3, "1") &&
        field_equals(cursor, 2, 4, "null") &&
        field_equals(cursor, 2, 5, "-1");

    printf("sdk_version=%s\n", cubesql_version());
    printf("empty.cursor_pointer=%s\n", empty_value == NULL ? "NULL" : "NON_NULL");
    printf("empty.cursor_length=%d\n", empty_length);
    printf("empty.sql_is_null=0\n");
    printf("empty.sql_typeof=blob\n");
    printf("empty.sql_length=0\n");
    printf("null.cursor_pointer=%s\n", null_value == NULL ? "NULL" : "NON_NULL");
    printf("null.cursor_length=%d\n", null_length);
    printf("null.sql_is_null=1\n");
    printf("null.sql_typeof=null\n");
    printf("null.sql_length=-1\n");

    if (!predicates_match) {
        fprintf(stderr, "Server-side predicates did not match expected SQL semantics.\n");
        return -1;
    }

    bug_reproduced = empty_value == NULL && empty_length == -1 &&
                     null_value == NULL && null_length == -1;
    printf("bug_reproduced=%d\n", bug_reproduced);
    return bug_reproduced;
}

int main(void) {
    struct config config;
    csqldb *db = NULL;
    csqlc *cursor = NULL;
    int reproduction = -1;
    int cleanup_ok;

    if (!load_config(&config)) return 2;
    if (!cleanup_database(&config)) return 2;

    db = connect_server(&config);
    if (db == NULL) return 2;
    if (!execute(db, "CREATE DATABASE '" SANDBOX_DATABASE "' IF NOT EXISTS;",
                 "sandbox creation")) goto done;
    if (cubesql_set_database(db, SANDBOX_DATABASE) != CUBESQL_NOERR) {
        fprintf(stderr, "Selecting sandbox failed with code %d: %s\n",
                cubesql_errcode(db), cubesql_errmsg(db));
        goto done;
    }
    if (!execute(db, "CREATE TABLE values_test (label TEXT, payload BLOB);",
                 "table creation")) goto done;
    if (!execute(db,
                 "INSERT INTO values_test VALUES ('empty', X''), ('null', NULL);",
                 "fixture insert")) goto done;
    if (cubesql_commit(db) != CUBESQL_NOERR) {
        fprintf(stderr, "Fixture commit failed with code %d: %s\n",
                cubesql_errcode(db), cubesql_errmsg(db));
        goto done;
    }

    cursor = cubesql_select(
        db,
        "SELECT label, payload, payload IS NULL, typeof(payload), "
        "coalesce(length(payload), -1) FROM values_test ORDER BY label;",
        0);
    if (cursor == NULL) {
        fprintf(stderr, "Fixture query failed with code %d: %s\n",
                cubesql_errcode(db), cubesql_errmsg(db));
        goto done;
    }
    reproduction = inspect_cursor(cursor);

done:
    if (cursor != NULL) cubesql_cursor_free(cursor);
    if (db != NULL) {
        cubesql_rollback(db);
        cubesql_disconnect(db, kTRUE);
    }
    cleanup_ok = cleanup_database(&config) && verify_database_absent(&config);
    printf("cleanup_verified=%d\n", cleanup_ok);

    if (!cleanup_ok || reproduction < 0) return 2;
    return reproduction == 1 ? 0 : 1;
}
