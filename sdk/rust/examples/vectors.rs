use licentivo::verify_lease;
use serde_json::Value;

fn main() {
    let path = std::env::args().nth(1).unwrap_or_else(|| "tests/lease-vectors.json".into());
    let fixture: Value = serde_json::from_slice(&std::fs::read(path).unwrap()).unwrap();
    let vectors = fixture["vectors"].as_array().unwrap();
    for vector in vectors {
        let actual = verify_lease(&vector["envelope"], &fixture["trusted_keys"],
            vector["product_id"].as_str().unwrap(), vector["device_id"].as_str().unwrap(),
            vector["now"].as_i64().unwrap()).unwrap();
        assert_eq!(actual, vector["valid"].as_bool().unwrap(), "{}", vector["name"]);
    }
    println!("Rust security core: {} lease vectors passed", vectors.len());
}
