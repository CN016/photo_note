/** 交付 DTO 不包含内部数据库主键、物理文件 key 或分享摘要。 */
export interface DeliverySettings {
 title: string; customer: string; description: string; internal_note: string
 limit_mode: 'unlimited' | 'max' | 'exact'; selection_limit: number
 deadline: string | null; expires_at: string | null
 allow_original: boolean; allow_download: boolean; access_code?: string; clear_access_code?: boolean
}
export interface DeliveryTask extends DeliverySettings {
 business_id: string; stage: string; status: string; version: number; updated_at: string
 shared: boolean; has_access_code: boolean; current_round: number; received_at: string | null
}
export interface DeliveryPhoto {
 business_id: string; task_id: string; kind: 'proof' | 'final'; filename: string; group_name: string
 bytes: number; preview_bytes: number; mark: 'pending' | 'keep' | 'exclude'; source_id: string | null; published_batch: number
}
export interface DeliveryRound {
 number: number; version: number; status: string; photo_ids: string[]; selected_ids: string[]
 notes: Record<string, string> | null; limit_mode: string; selection_limit: number; deadline: string | null; submitted_at: string | null
}
export interface DeliveryDetail {
 task: DeliveryTask; photos: DeliveryPhoto[]; rounds: DeliveryRound[]
 events: { action: string; stage: string; at: string }[]
}
export interface DeliveryCommand { version: number; action: string; value?: string; photo_ids?: string[]; source_id?: string | null; settings?: DeliverySettings }
export interface DeliverySelection { round: number; version: number; selected_ids: string[]; notes: Record<string,string>; submit: boolean }

/** 打包接受与完成分离，状态轮询展示实际文件进度。 */
export interface DeliveryExport {business_id:string;status:string;progress:number;total:number;bytes:number;error_code:string}
