package love.imcn016.photo;
import java.io.*;
/** Android 9也可用的流复制，避免调用新系统才提供的InputStream辅助API。 */
final class BundleIO {
    static void copy(InputStream input, OutputStream output) throws IOException {
        byte[] buffer = new byte[65536]; for (int n; (n = input.read(buffer)) != -1;) output.write(buffer, 0, n);
    }
    static byte[] read(InputStream input) throws IOException {
        ByteArrayOutputStream output = new ByteArrayOutputStream(); copy(input, output); return output.toByteArray();
    }
}
