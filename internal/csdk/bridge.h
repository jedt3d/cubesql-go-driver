#ifndef CUBESQL_GO_BRIDGE_H
#define CUBESQL_GO_BRIDGE_H

#include <stddef.h>
#include <stdint.h>

typedef struct csqlgo_conn csqlgo_conn;
typedef struct csqlgo_cursor csqlgo_cursor;
typedef struct csqlgo_stmt csqlgo_stmt;
typedef struct csqlgo_bind csqlgo_bind;

enum {
    CSQLGO_OK = 0,
    CSQLGO_FIELD_NULL = 1,
    CSQLGO_ERR_INVALID = -10000,
    CSQLGO_ERR_MEMORY = -10001
};

const char *csqlgo_version(void);
void csqlgo_free(void *ptr);

int csqlgo_conn_open(csqlgo_conn **out, const char *host, int port,
                     const char *username, const char *password, int timeout,
                     int encryption, char **error_message);
void csqlgo_conn_close(csqlgo_conn *conn, int gracefully);
int csqlgo_conn_ping(csqlgo_conn *conn);
int csqlgo_conn_execute(csqlgo_conn *conn, const char *sql);
int csqlgo_conn_set_database(csqlgo_conn *conn, const char *name);
int csqlgo_conn_begin(csqlgo_conn *conn);
int csqlgo_conn_commit(csqlgo_conn *conn);
int csqlgo_conn_rollback(csqlgo_conn *conn);
int64_t csqlgo_conn_changes(csqlgo_conn *conn);
int64_t csqlgo_conn_affected_rows(csqlgo_conn *conn);
int64_t csqlgo_conn_last_insert_id(csqlgo_conn *conn);
int csqlgo_conn_error_code(csqlgo_conn *conn);
char *csqlgo_conn_error_message_copy(csqlgo_conn *conn);

int csqlgo_conn_query(csqlgo_conn *conn, const char *sql,
                      csqlgo_cursor **out);
void csqlgo_cursor_close(csqlgo_cursor *cursor);
int csqlgo_cursor_num_rows(csqlgo_cursor *cursor);
int csqlgo_cursor_num_columns(csqlgo_cursor *cursor);
int csqlgo_cursor_column_type(csqlgo_cursor *cursor, int column);
int csqlgo_cursor_seek(csqlgo_cursor *cursor, int row);
int csqlgo_cursor_copy_field(csqlgo_cursor *cursor, int row, int column,
                             unsigned char **out, int *length);
int csqlgo_cursor_copy_column_name(csqlgo_cursor *cursor, int column,
                                   unsigned char **out, int *length);

csqlgo_bind *csqlgo_bind_new(int count);
void csqlgo_bind_close(csqlgo_bind *bind);
int csqlgo_bind_set_int64(csqlgo_bind *bind, int index, int64_t value);
int csqlgo_bind_set_double(csqlgo_bind *bind, int index, double value);
int csqlgo_bind_set_text(csqlgo_bind *bind, int index, const void *value,
                         int length);
int csqlgo_bind_set_blob(csqlgo_bind *bind, int index, const void *value,
                         int length);
int csqlgo_bind_set_null(csqlgo_bind *bind, int index);
int csqlgo_conn_execute_bind(csqlgo_conn *conn, const char *sql,
                             csqlgo_bind *bind);

int csqlgo_conn_prepare(csqlgo_conn *conn, const char *sql,
                        csqlgo_stmt **out);
int csqlgo_stmt_close(csqlgo_stmt *stmt);
int csqlgo_stmt_bind_int64(csqlgo_stmt *stmt, int index, int64_t value);
int csqlgo_stmt_bind_double(csqlgo_stmt *stmt, int index, double value);
int csqlgo_stmt_bind_text(csqlgo_stmt *stmt, int index, const void *value,
                          int length);
int csqlgo_stmt_bind_blob(csqlgo_stmt *stmt, int index, const void *value,
                          int length);
int csqlgo_stmt_bind_null(csqlgo_stmt *stmt, int index);
int csqlgo_stmt_bind_zeroblob(csqlgo_stmt *stmt, int index, int length);
int csqlgo_stmt_execute(csqlgo_stmt *stmt);
int csqlgo_stmt_query(csqlgo_stmt *stmt, csqlgo_cursor **out);

#endif
