// queue-verify 仅针对显式指定的独立测试库和测试用户，测量 API worker 的真实队列与数据库写回。
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/lib/pq"
	"os"
	"time"
)

func main() {
	dsn := os.Getenv("PHOTO_NINI_TENANT_TEST_DSN")
	uid := os.Getenv("PHOTO_NINI_QUEUE_TEST_USER")
	if dsn == "" || uid == "" {
		panic("必须显式指定独立测试库及测试用户")
	}
	db, e := sql.Open("postgres", dsn)
	check(e)
	defer db.Close()
	ctx := context.Background()
	var name string
	check(db.QueryRow("SELECT current_database()").Scan(&name))
	if name != "photo_nini_test" {
		panic("仅允许 photo_nini_test 独立测试库")
	}
	// 暂停领取，先把已压缩成功的十张测试图片恢复为等待识别；不计入上传和压缩时间。
	_, e = db.Exec(`UPDATE app_configs SET config_value=jsonb_set(config_value::jsonb,'{enabled}','false')::text WHERE config_key='inspirations.processing' AND orgid=-1`)
	check(e)
	defer db.Exec(`UPDATE app_configs SET config_value=jsonb_set(config_value::jsonb,'{enabled}','true')::text WHERE config_key='inspirations.processing' AND orgid=-1`)
	time.Sleep(2 * time.Second)
	var results []map[string]any
	for round := 0; round < 5; round++ {
		tx, e := db.BeginTx(ctx, nil)
		check(e)
		res, e := tx.Exec(`UPDATE inspiration_processing_jobs SET stage='pending_analysis',attempts=0,next_attempt_at=now(),lease_token=NULL,lease_until=NULL,error_code='' WHERE orgid=-1 AND user_id=$1 AND algorithm='yolox-nano-v1/color-v1'`, uid)
		check(e)
		n, e := res.RowsAffected()
		check(e)
		if n != 10 {
			panic(fmt.Sprintf("需要十张测试图片，实际 %d", n))
		}
		_, e = tx.Exec(`UPDATE inspirations SET analysis_status='pending_analysis',people_count=NULL,is_empty_shot=NULL WHERE orgid=-1 AND user_id=$1`, uid)
		check(e)
		check(tx.Commit())
		start := time.Now()
		_, e = db.Exec(`UPDATE app_configs SET config_value=jsonb_set(config_value::jsonb,'{enabled}','true')::text WHERE config_key='inspirations.processing' AND orgid=-1`)
		check(e)
		peak, completed := 0, 0
		for time.Since(start) < 10*time.Second {
			var active int
			check(db.QueryRow(`SELECT count(*) FILTER(WHERE stage='analyzing'),count(*) FILTER(WHERE stage='succeeded') FROM inspiration_processing_jobs WHERE orgid=-1 AND user_id=$1`, uid).Scan(&active, &completed))
			if active > peak {
				peak = active
			}
			if completed == 10 {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		seconds := time.Since(start).Seconds()
		if completed != 10 {
			panic("十秒内结果未全部落库")
		}
		results = append(results, map[string]any{"round": round + 1, "seconds_including_db_save": seconds, "peak_active_analysis_leases": peak, "completed": completed})
		_, e = db.Exec(`UPDATE app_configs SET config_value=jsonb_set(config_value::jsonb,'{enabled}','false')::text WHERE config_key='inspirations.processing' AND orgid=-1`)
		check(e)
		time.Sleep(1100 * time.Millisecond)
	}
	output, e := json.MarshalIndent(results, "", "  ")
	check(e)
	fmt.Println(string(output))
}
func check(e error) {
	if e != nil {
		panic(e)
	}
}
