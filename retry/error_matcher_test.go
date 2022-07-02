package retry

import (
	"testing"

	githubMysql "github.com/go-sql-driver/mysql"
)

func TestIsDuplicateEntryError(t *testing.T) {
	type args struct {
		err                error
		duplicateEntryName string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "github error match",
			args: args{
				err: &githubMysql.MySQLError{
					Number:  1062,
					Message: "Duplicate entry 'a61a860e16574e6094d9d5766d505058-1' for key 'octopus_meta.uniq_biz_id_source'",
				},
				duplicateEntryName: "octopus_meta.uniq_biz_id_source",
			},
			want: true,
		}, {
			name: "github error not match",
			args: args{
				err: &githubMysql.MySQLError{
					Number:  1062,
					Message: "Duplicate entry 'a61a860e16574e6094d9d5766d505058-1' for key 'octopus_meta.uniq_octopus_id'",
				},
				duplicateEntryName: "octopus_meta.uniq_biz_id_source",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDuplicateEntryError(tt.args.err, tt.args.duplicateEntryName); got != tt.want {
				t.Errorf("IsDuplicateEntryError() = %v, want %v", got, tt.want)
			}
		})
	}
}
