package love.imcn016.photo;
import org.junit.Test;
import static org.junit.Assert.*;

/** 验证通知历史不重放、重复轮询去重，以及未读变化不会误触发通知。 */
public class NotificationBaselineTest {
    @Test public void firstLoadAndRepeatedLatestDoNotNotify() {
        NotificationBaseline state = new NotificationBaseline();
        assertFalse(state.update("history", 8)); assertFalse(state.update("history", 9));
        assertTrue(state.update("new", 10)); assertFalse(state.update("new", 10));
    }
    @Test public void emptyBaselineCanReceiveFirstNewMessage() {
        NotificationBaseline state = new NotificationBaseline();
        assertFalse(state.update("", 0)); assertTrue(state.update("first", 1));
        assertFalse(state.update("already-read", 0)); assertFalse(state.update("", 0));
    }
    @Test public void newSpaceStartsWithIndependentBaseline() {
        NotificationBaseline old = new NotificationBaseline(); old.update("old-space", 1);
        assertFalse(new NotificationBaseline().update("new-space-history", 9));
    }
}
