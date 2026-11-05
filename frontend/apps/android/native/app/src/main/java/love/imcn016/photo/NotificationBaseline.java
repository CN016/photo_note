package love.imcn016.photo;

/** 仅保存最新消息UUID作为变化基线；首次进入不重放历史，多条消息合并为通用提醒。 */
final class NotificationBaseline {
    private boolean ready;
    private String latest = "";
    boolean update(String id, long unread) {
        boolean changed = ready && !id.isEmpty() && !id.equals(latest) && unread > 0;
        latest = id; ready = true;
        return changed;
    }
}
