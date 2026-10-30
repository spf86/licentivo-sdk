package licentivo;

import java.nio.file.*;
import java.util.*;

public final class TestVectors {
    public static void main(String[] args) throws Exception {
        var fixture = Json.object(Json.parse(Files.readString(Path.of(args.length > 0 ? args[0] : "tests/lease-vectors.json")), 131072));
        Map<String, String> keys = new HashMap<>();
        Json.object(fixture.get("trusted_keys")).forEach((k, v) -> keys.put(k, (String) v));
        int count = 0;
        for (Object item : (List<?>) fixture.get("vectors")) {
            var vector = Json.object(item);
            boolean valid;
            try {
                Client.verify(Json.object(vector.get("envelope")), keys, Json.text(vector, "product_id"), Json.text(vector, "device_id"), Json.number(vector, "now"));
                valid = true;
            } catch (Exception error) {
                valid = false;
            }
            if (valid != Boolean.TRUE.equals(vector.get("valid"))) throw new AssertionError(vector.get("name"));
            count++;
        }
        // Public initialization does not require a display-name setting or
        // network access. An unactivated client must keep features denied.
        Path folder = Files.createTempDirectory("licentivo-java-device-default-");
        Map<String, Object> config = new LinkedHashMap<>();
        config.put("base_url", "https://sdk-defaults.invalid");
        config.put("product_id", "10000000-0000-4000-8000-000000000001");
        config.put("trusted_keys", keys);
        config.put("license_key", "lv_lic_" + "A".repeat(43));
        config.put("cache_path", folder.resolve("lease.json").toString());
        try (Client client = new Client(config)) {
            if (Boolean.TRUE.equals(client.featureStatus("export").get("allowed")))
                throw new AssertionError("Unactivated initialization granted a feature");
        }
        if (config.containsKey("device_name")) throw new AssertionError("Initialization mutated caller configuration");
        System.out.println("Java: " + count + " lease vectors and optional device-name initialization passed");
    }
}
