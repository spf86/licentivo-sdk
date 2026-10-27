/* Include the private implementation to exercise the production date parser. */
#include "licentivo.c"

int main(void) {
    const struct { const char *text; double timestamp; } valid[] = {
        {"1970-01-01T00:00:00Z", 0},
        {"1970-01-01T00:00:00.123456789Z", .123456789},
        {"1970-01-01T08:00:00+08:00", 0},
        {"1969-12-31T18:30:00-05:30", 0},
        {"2000-01-01T00:00:00Z", 946684800},
        {"2000-02-29T00:00:00Z", 951782400},
        {"2000-02-29T01:00:00.5+01:00", 951782400.5},
        {"9999-12-31T23:59:59Z", 253402300799}
    };
    const char *invalid[] = {
        NULL, "", "not-a-date", "0000-01-01T00:00:00Z",
        "2026-02-29T00:00:00Z", "1900-02-29T00:00:00Z",
        "2026-04-31T00:00:00Z", "2026-00-01T00:00:00Z",
        "2026-13-01T00:00:00Z", "2026-01-00T00:00:00Z",
        "2026-01-01T24:00:00Z", "2026-01-01T00:60:00Z",
        "2026-01-01T00:00:60Z", "2026-01-01T00:00:00",
        "2026-01-01T00:00:00.Z", "2026-01-01T00:00:00.1234567890Z",
        "2026-01-01T00:00:00+24:00", "2026-01-01T00:00:00+01:60",
        "2026-01-01T00:00:00+01", "2026-01-01T00:00:00Ztrailing"
    };
    for (size_t i=0;i<sizeof(valid)/sizeof(valid[0]);i++) {
        double timestamp=0;
        if (!task_deadline(valid[i].text,&timestamp) || fabs(timestamp-valid[i].timestamp)>.00001) {
            fprintf(stderr,"Reservation timestamp failed: %s -> %.9f\n",valid[i].text,timestamp);return 1;
        }
    }
    for (size_t i=0;i<sizeof(invalid)/sizeof(invalid[0]);i++) {
        double timestamp=0;
        if (task_deadline(invalid[i],&timestamp)) {
            fprintf(stderr,"Invalid reservation timestamp accepted: %s\n",invalid[i]);return 1;
        }
    }
    if (!isfinite(task_wall_time()) || fabs(task_wall_time()-(double)time(NULL))>2) return 1;
    puts("C/C++/Rust/Ruby: 28 reservation deadline vectors passed");
    return 0;
}
