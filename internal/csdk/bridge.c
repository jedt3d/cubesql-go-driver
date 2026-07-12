#include "bridge.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "../../third_party/cubesql-sdk/cubesql.h"

struct csqlgo_conn {
    csqldb *db;
};

struct csqlgo_cursor {
    csqlc *cursor;
};

struct csqlgo_stmt {
    csqlvm *vm;
    csqlgo_conn *owner;
};

struct csqlgo_bind {
    int count;
    char **values;
    int *sizes;
    int *types;
};

static char *csqlgo_copy_string(const char *value) {
    size_t length;
    char *copy;

    if (value == NULL) return NULL;
    length = strlen(value);
    copy = (char *)malloc(length + 1);
    if (copy == NULL) return NULL;
    memcpy(copy, value, length + 1);
    return copy;
}

static int csqlgo_valid_conn(csqlgo_conn *conn) {
    return conn != NULL && conn->db != NULL;
}

static int csqlgo_bind_set_bytes(csqlgo_bind *bind, int index,
                                 const void *value, int length, int type) {
    int slot;
    size_t allocation;
    char *copy = NULL;

    if (bind == NULL || index <= 0 || index > bind->count || length < 0)
        return CSQLGO_ERR_INVALID;
    if (value == NULL && length > 0)
        return CSQLGO_ERR_INVALID;

    slot = index - 1;
    if (type != CUBESQL_BIND_NULL) {
        if (type == CUBESQL_BIND_BLOB)
            allocation = length > 0 ? (size_t)length : 1;
        else
            allocation = (size_t)length + 1;
        copy = (char *)calloc(1, allocation);
        if (copy == NULL) return CSQLGO_ERR_MEMORY;
        if (length > 0 && value != NULL) memcpy(copy, value, (size_t)length);
    }

    free(bind->values[slot]);
    bind->values[slot] = copy;
    bind->sizes[slot] = length;
    bind->types[slot] = type;
    return CSQLGO_OK;
}

const char *csqlgo_version(void) {
    return cubesql_version();
}

void csqlgo_free(void *ptr) {
    free(ptr);
}

int csqlgo_conn_open(csqlgo_conn **out, const char *host, int port,
                     const char *username, const char *password, int timeout,
                     int encryption, char **error_message) {
    csqlgo_conn *conn;
    int result;

    if (out == NULL || error_message == NULL) return CSQLGO_ERR_INVALID;
    *out = NULL;
    *error_message = NULL;

    conn = (csqlgo_conn *)calloc(1, sizeof(*conn));
    if (conn == NULL) return CSQLGO_ERR_MEMORY;

    result = cubesql_connect(&conn->db, host, port, username, password, timeout,
                             encryption);
    if (result != CUBESQL_NOERR) {
        if (conn->db != NULL) {
            *error_message = csqlgo_copy_string(cubesql_errmsg(conn->db));
            cubesql_disconnect(conn->db, kFALSE);
        }
        free(conn);
        return result;
    }

    *out = conn;
    return CSQLGO_OK;
}

void csqlgo_conn_close(csqlgo_conn *conn, int gracefully) {
    if (conn == NULL) return;
    if (conn->db != NULL) {
        cubesql_disconnect(conn->db, gracefully ? kTRUE : kFALSE);
        conn->db = NULL;
    }
    free(conn);
}

int csqlgo_conn_ping(csqlgo_conn *conn) {
    if (!csqlgo_valid_conn(conn)) return CSQLGO_ERR_INVALID;
    return cubesql_ping(conn->db);
}

int csqlgo_conn_execute(csqlgo_conn *conn, const char *sql) {
    if (!csqlgo_valid_conn(conn) || sql == NULL) return CSQLGO_ERR_INVALID;
    return cubesql_execute(conn->db, sql);
}

int csqlgo_conn_set_database(csqlgo_conn *conn, const char *name) {
    if (!csqlgo_valid_conn(conn)) return CSQLGO_ERR_INVALID;
    return cubesql_set_database(conn->db, name);
}

int csqlgo_conn_begin(csqlgo_conn *conn) {
    if (!csqlgo_valid_conn(conn)) return CSQLGO_ERR_INVALID;
    return cubesql_begintransaction(conn->db);
}

int csqlgo_conn_commit(csqlgo_conn *conn) {
    if (!csqlgo_valid_conn(conn)) return CSQLGO_ERR_INVALID;
    return cubesql_commit(conn->db);
}

int csqlgo_conn_rollback(csqlgo_conn *conn) {
    if (!csqlgo_valid_conn(conn)) return CSQLGO_ERR_INVALID;
    return cubesql_rollback(conn->db);
}

int64_t csqlgo_conn_changes(csqlgo_conn *conn) {
    if (!csqlgo_valid_conn(conn)) return 0;
    return (int64_t)cubesql_changes(conn->db);
}

int64_t csqlgo_conn_affected_rows(csqlgo_conn *conn) {
    if (!csqlgo_valid_conn(conn)) return 0;
    return (int64_t)cubesql_affected_rows(conn->db);
}

int64_t csqlgo_conn_last_insert_id(csqlgo_conn *conn) {
    if (!csqlgo_valid_conn(conn)) return 0;
    return (int64_t)cubesql_last_inserted_rowID(conn->db);
}

int csqlgo_conn_error_code(csqlgo_conn *conn) {
    if (!csqlgo_valid_conn(conn)) return CSQLGO_ERR_INVALID;
    return cubesql_errcode(conn->db);
}

char *csqlgo_conn_error_message_copy(csqlgo_conn *conn) {
    if (!csqlgo_valid_conn(conn)) return NULL;
    return csqlgo_copy_string(cubesql_errmsg(conn->db));
}

int csqlgo_conn_query(csqlgo_conn *conn, const char *sql,
                      csqlgo_cursor **out) {
    csqlc *cursor;
    csqlgo_cursor *wrapper;

    if (!csqlgo_valid_conn(conn) || sql == NULL || out == NULL)
        return CSQLGO_ERR_INVALID;
    *out = NULL;
    cursor = cubesql_select(conn->db, sql, kFALSE);
    if (cursor == NULL) {
        int code = cubesql_errcode(conn->db);
        return code == CUBESQL_NOERR ? CUBESQL_ERR : code;
    }
    wrapper = (csqlgo_cursor *)malloc(sizeof(*wrapper));
    if (wrapper == NULL) {
        cubesql_cursor_free(cursor);
        return CSQLGO_ERR_MEMORY;
    }
    wrapper->cursor = cursor;
    *out = wrapper;
    return CSQLGO_OK;
}

void csqlgo_cursor_close(csqlgo_cursor *cursor) {
    if (cursor == NULL) return;
    if (cursor->cursor != NULL) {
        cubesql_cursor_free(cursor->cursor);
        cursor->cursor = NULL;
    }
    free(cursor);
}

int csqlgo_cursor_num_rows(csqlgo_cursor *cursor) {
    if (cursor == NULL || cursor->cursor == NULL) return CSQLGO_ERR_INVALID;
    return cubesql_cursor_numrows(cursor->cursor);
}

int csqlgo_cursor_num_columns(csqlgo_cursor *cursor) {
    if (cursor == NULL || cursor->cursor == NULL) return CSQLGO_ERR_INVALID;
    return cubesql_cursor_numcolumns(cursor->cursor);
}

int csqlgo_cursor_column_type(csqlgo_cursor *cursor, int column) {
    if (cursor == NULL || cursor->cursor == NULL) return CSQLGO_ERR_INVALID;
    return cubesql_cursor_columntype(cursor->cursor, column);
}

int csqlgo_cursor_seek(csqlgo_cursor *cursor, int row) {
    if (cursor == NULL || cursor->cursor == NULL) return CSQLGO_ERR_INVALID;
    return cubesql_cursor_seek(cursor->cursor, row);
}

int csqlgo_cursor_copy_field(csqlgo_cursor *cursor, int row, int column,
                             unsigned char **out, int *length) {
    char *field;
    int field_length = 0;
    size_t allocation;

    if (cursor == NULL || cursor->cursor == NULL || out == NULL || length == NULL)
        return CSQLGO_ERR_INVALID;
    *out = NULL;
    *length = 0;
    field = cubesql_cursor_field(cursor->cursor, row, column, &field_length);
    if (field == NULL) {
        if (field_length == -1) return CSQLGO_FIELD_NULL;
        return CSQLGO_ERR_INVALID;
    }
    allocation = field_length > 0 ? (size_t)field_length : 1;
    *out = (unsigned char *)malloc(allocation);
    if (*out == NULL) return CSQLGO_ERR_MEMORY;
    if (field_length > 0) memcpy(*out, field, (size_t)field_length);
    *length = field_length;
    return CSQLGO_OK;
}

int csqlgo_cursor_copy_column_name(csqlgo_cursor *cursor, int column,
                                   unsigned char **out, int *length) {
    return csqlgo_cursor_copy_field(cursor, CUBESQL_COLNAME, column, out,
                                    length);
}

csqlgo_bind *csqlgo_bind_new(int count) {
    csqlgo_bind *bind;
    if (count <= 0) return NULL;
    bind = (csqlgo_bind *)calloc(1, sizeof(*bind));
    if (bind == NULL) return NULL;
    bind->values = (char **)calloc((size_t)count, sizeof(*bind->values));
    bind->sizes = (int *)calloc((size_t)count, sizeof(*bind->sizes));
    bind->types = (int *)calloc((size_t)count, sizeof(*bind->types));
    if (bind->values == NULL || bind->sizes == NULL || bind->types == NULL) {
        csqlgo_bind_close(bind);
        return NULL;
    }
    bind->count = count;
    return bind;
}

void csqlgo_bind_close(csqlgo_bind *bind) {
    int index;
    if (bind == NULL) return;
    if (bind->values != NULL) {
        for (index = 0; index < bind->count; index++) free(bind->values[index]);
    }
    free(bind->values);
    free(bind->sizes);
    free(bind->types);
    free(bind);
}

int csqlgo_bind_set_int64(csqlgo_bind *bind, int index, int64_t value) {
    char buffer[64];
    int length = snprintf(buffer, sizeof(buffer), "%lld", (long long)value);
    if (length < 0 || length >= (int)sizeof(buffer)) return CSQLGO_ERR_INVALID;
    return csqlgo_bind_set_bytes(bind, index, buffer, length,
                                 CUBESQL_BIND_INT64);
}

int csqlgo_bind_set_double(csqlgo_bind *bind, int index, double value) {
    char buffer[64];
    int length = snprintf(buffer, sizeof(buffer), "%.17g", value);
    if (length < 0 || length >= (int)sizeof(buffer)) return CSQLGO_ERR_INVALID;
    return csqlgo_bind_set_bytes(bind, index, buffer, length,
                                 CUBESQL_BIND_DOUBLE);
}

int csqlgo_bind_set_text(csqlgo_bind *bind, int index, const void *value,
                         int length) {
    return csqlgo_bind_set_bytes(bind, index, value, length,
                                 CUBESQL_BIND_TEXT);
}

int csqlgo_bind_set_blob(csqlgo_bind *bind, int index, const void *value,
                         int length) {
    if (length <= 0) return CSQLGO_ERR_INVALID;
    return csqlgo_bind_set_bytes(bind, index, value, length,
                                 CUBESQL_BIND_BLOB);
}

int csqlgo_bind_set_null(csqlgo_bind *bind, int index) {
    return csqlgo_bind_set_bytes(bind, index, NULL, 0, CUBESQL_BIND_NULL);
}

int csqlgo_conn_execute_bind(csqlgo_conn *conn, const char *sql,
                             csqlgo_bind *bind) {
    int index;
    int result;
    char **values;
    int *sizes;
    int *types;
    if (!csqlgo_valid_conn(conn) || sql == NULL || bind == NULL)
        return CSQLGO_ERR_INVALID;
    for (index = 0; index < bind->count; index++) {
        if (bind->types[index] == 0) return CSQLGO_ERR_INVALID;
    }
    values = (char **)malloc((size_t)bind->count * sizeof(*values));
    sizes = (int *)malloc((size_t)bind->count * sizeof(*sizes));
    types = (int *)malloc((size_t)bind->count * sizeof(*types));
    if (values == NULL || sizes == NULL || types == NULL) {
        free(values);
        free(sizes);
        free(types);
        return CSQLGO_ERR_MEMORY;
    }
    memcpy(values, bind->values, (size_t)bind->count * sizeof(*values));
    memcpy(sizes, bind->sizes, (size_t)bind->count * sizeof(*sizes));
    memcpy(types, bind->types, (size_t)bind->count * sizeof(*types));
    result = cubesql_bind(conn->db, sql, values, sizes, types, bind->count);
    free(values);
    free(sizes);
    free(types);
    return result;
}

int csqlgo_conn_prepare(csqlgo_conn *conn, const char *sql,
                        csqlgo_stmt **out) {
    csqlgo_stmt *stmt;
    csqlvm *vm;
    if (!csqlgo_valid_conn(conn) || sql == NULL || out == NULL)
        return CSQLGO_ERR_INVALID;
    *out = NULL;
    vm = cubesql_vmprepare(conn->db, sql);
    if (vm == NULL) {
        int code = cubesql_errcode(conn->db);
        return code == CUBESQL_NOERR ? CUBESQL_ERR : code;
    }
    stmt = (csqlgo_stmt *)malloc(sizeof(*stmt));
    if (stmt == NULL) {
        cubesql_vmclose(vm);
        return CSQLGO_ERR_MEMORY;
    }
    stmt->vm = vm;
    stmt->owner = conn;
    *out = stmt;
    return CSQLGO_OK;
}

int csqlgo_stmt_close(csqlgo_stmt *stmt) {
    int result = CSQLGO_OK;
    if (stmt == NULL) return CSQLGO_OK;
    if (stmt->vm != NULL) {
        result = cubesql_vmclose(stmt->vm);
        stmt->vm = NULL;
    }
    free(stmt);
    return result;
}

int csqlgo_stmt_bind_int64(csqlgo_stmt *stmt, int index, int64_t value) {
    if (stmt == NULL || stmt->vm == NULL) return CSQLGO_ERR_INVALID;
    return cubesql_vmbind_int64(stmt->vm, index, (int64)value);
}

int csqlgo_stmt_bind_double(csqlgo_stmt *stmt, int index, double value) {
    if (stmt == NULL || stmt->vm == NULL) return CSQLGO_ERR_INVALID;
    return cubesql_vmbind_double(stmt->vm, index, value);
}

int csqlgo_stmt_bind_text(csqlgo_stmt *stmt, int index, const void *value,
                          int length) {
    if (stmt == NULL || stmt->vm == NULL || length < 0)
        return CSQLGO_ERR_INVALID;
    return cubesql_vmbind_text(stmt->vm, index, (char *)value, length);
}

int csqlgo_stmt_bind_blob(csqlgo_stmt *stmt, int index, const void *value,
                          int length) {
    if (stmt == NULL || stmt->vm == NULL || value == NULL || length <= 0)
        return CSQLGO_ERR_INVALID;
    return cubesql_vmbind_blob(stmt->vm, index, (void *)value, length);
}

int csqlgo_stmt_bind_null(csqlgo_stmt *stmt, int index) {
    if (stmt == NULL || stmt->vm == NULL) return CSQLGO_ERR_INVALID;
    return cubesql_vmbind_null(stmt->vm, index);
}

int csqlgo_stmt_bind_zeroblob(csqlgo_stmt *stmt, int index, int length) {
    if (stmt == NULL || stmt->vm == NULL || length <= 0)
        return CSQLGO_ERR_INVALID;
    return cubesql_vmbind_zeroblob(stmt->vm, index, length);
}

int csqlgo_stmt_execute(csqlgo_stmt *stmt) {
    if (stmt == NULL || stmt->vm == NULL) return CSQLGO_ERR_INVALID;
    return cubesql_vmexecute(stmt->vm);
}

int csqlgo_stmt_query(csqlgo_stmt *stmt, csqlgo_cursor **out) {
    csqlc *cursor;
    csqlgo_cursor *wrapper;
    if (stmt == NULL || stmt->vm == NULL || out == NULL)
        return CSQLGO_ERR_INVALID;
    *out = NULL;
    cursor = cubesql_vmselect(stmt->vm);
    if (cursor == NULL) {
        int code = csqlgo_conn_error_code(stmt->owner);
        return code == CUBESQL_NOERR ? CUBESQL_ERR : code;
    }
    wrapper = (csqlgo_cursor *)malloc(sizeof(*wrapper));
    if (wrapper == NULL) {
        cubesql_cursor_free(cursor);
        return CSQLGO_ERR_MEMORY;
    }
    wrapper->cursor = cursor;
    *out = wrapper;
    return CSQLGO_OK;
}
