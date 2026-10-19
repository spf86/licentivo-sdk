using Licentivo;
namespace LicentivoDemo;
sealed class DemoForm : Form {
    // BEGIN LICENTIVO PRODUCT
    // Console > Products > Signing public keys: paste Product ID and every key ID/public key pair.
    const string ProductId = "YOUR_PRODUCT_ID";
    static readonly Dictionary<string,string> TrustedKeys = new() { ["YOUR_KEY_ID"] = "YOUR_PUBLIC_KEY" };
    // END LICENTIVO PRODUCT
    readonly TextBox key = new() { Width = 380, UseSystemPasswordChar = true };
    readonly Label status = new() { AutoSize = true, MaximumSize = new Size(550, 0), Text = "Enter a key, or leave empty to restore this device." };
    readonly CancellationTokenSource lifetime = new();
    LicentivoClient? client;
    bool busy;
    [STAThread] static void Main() { ApplicationConfiguration.Initialize(); Application.Run(new DemoForm()); }
    DemoForm() {
        Text = "Licentivo · Report export"; ClientSize = new Size(600, 220);
        var panel = new FlowLayoutPanel { Dock = DockStyle.Fill, Padding = new Padding(20), FlowDirection = FlowDirection.TopDown, WrapContents = false, AutoScroll = true };
        Controls.Add(panel); panel.Controls.Add(new Label { Text = "Customer license key", AutoSize = true }); panel.Controls.Add(key);
        Add(panel, "Activate / restore", async () => {
            if (client is not null) { await client.DisposeAsync(); client = null; }
            client = await LicentivoClient.OpenProduct(ProductId, TrustedKeys, key.Text, ct: lifetime.Token);
            key.Clear(); status.Text = client.Status().State;
        });
        var buttons = new FlowLayoutPanel { AutoSize = true, AutoSizeMode = AutoSizeMode.GrowAndShrink, WrapContents = false }; panel.Controls.Add(buttons);
        Add(buttons, "Export CSV", () => Export("export", "report.csv"));
        Add(buttons, "Pro export", () => Export("pro", "pro-report.csv"));
        Add(buttons, "Refresh license", async () => { await Require().RefreshOnline(lifetime.Token); status.Text = Require().Status().State; });
        panel.Controls.Add(status);
        FormClosed += async (_, _) => { lifetime.Cancel(); if (client is not null) await client.DisposeAsync(); };
    }
    LicentivoClient Require() => client ?? throw new InvalidOperationException("Activate the application first.");
    async Task Export(string feature, string filename) {
        using var save = new SaveFileDialog { FileName = filename, Filter = "CSV report|*.csv" };
        await Require().RunFeature(feature, async () => {
            if (save.ShowDialog(this) != DialogResult.OK) return;
            await File.WriteAllTextAsync(save.FileName, "Item,Quantity,Price\nNotebook,2,12.50\nPen,5,3.00\n");
            status.Text = "Saved: " + save.FileName;
        });
    }
    void Add(Control panel, string title, Func<Task> action) {
        var button = new Button { Text = title, AutoSize = true }; panel.Controls.Add(button);
        button.Click += async (_, _) => {
            if (busy) return; busy = true; status.Text = "Working…";
            try { await action(); } catch (Exception error) { if (!IsDisposed) status.Text = error.Message; }
            finally { busy = false; }
        };
    }
}
