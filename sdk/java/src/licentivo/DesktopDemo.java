package licentivo;
import javax.swing.*;
import java.awt.*;
import java.nio.file.*;
import java.util.*;
import java.util.concurrent.*;

/** Activation UI, live status and a protected export; network stays off the Swing event thread. */
public final class DesktopDemo {
// BEGIN LICENTIVO PRODUCT
// Copy Product ID and all key ID/public key pairs from Console > Products > Signing public keys.
private static final String PRODUCT_ID = "YOUR_PRODUCT_ID";
private static final Map<String,String> TRUSTED_KEYS = Map.of("YOUR_KEY_ID", "YOUR_PUBLIC_KEY");
// END LICENTIVO PRODUCT
 private final JFrame window=new JFrame("Licentivo | Application example");
 private final JPasswordField key=new JPasswordField();
 private final JLabel status=new JLabel("Activate once; leave the key empty on later starts.");
 private final JCheckBox metered=new JCheckBox("Export has a usage limit configured in the policy");
 private final ExecutorService worker=Executors.newSingleThreadExecutor();
 private final java.util.function.Supplier<Path> chooseOutput;
 private Client client;private String pendingID,pendingJob;
 private void work(Callable<Void> action){worker.submit(()->{try{action.call();}catch(Exception e){message(e.getMessage());}});}
 private void message(String value){SwingUtilities.invokeLater(()->status.setText(value));}
 private void activate(){String secret=new String(key.getPassword());work(()->{if(pendingID!=null)throw new Exception("Finish the pending commit before changing the client.");if(client!=null)client.close();client=null;client=Client.openProduct(PRODUCT_ID,TRUSTED_KEYS,secret,e->message(e.getMessage()));SwingUtilities.invokeLater(()->key.setText(""));client.onStatusChanged(s->message((String)s.get("state")));message((String)client.status().get("state"));return null;});}
 private void export(){Path output=chooseOutput.get();if(output==null)return;boolean limited=metered.isSelected();work(()->{if(client==null||!client.checkFeature("export"))throw new Exception("Export is not permitted. Activate or refresh first.");if(pendingID!=null)throw new Exception("Retry the pending commit before exporting again.");String job=UUID.randomUUID().toString();Map<String,Object>hold=limited?client.reserve("export",1,job):null;if(hold!=null&&!hold.get("status").equals("pending"))throw new Exception("Closed operation; do not export again.");try{client.runFeature("export",()->Files.writeString(output,"Item,Quantity,Price\nNotebook,2,12.50\nPen,5,3.00\n"));}catch(Exception e){if(hold!=null)client.cancel(Json.text(hold,"reservation_id"),job);throw e;}if(hold!=null){pendingID=Json.text(hold,"reservation_id");pendingJob=job;commit();}message("Export completed: "+output);return null;});}
 private void commit()throws Exception{if(pendingID==null)return;var receipt=client.commit(pendingID,pendingJob);pendingID=null;pendingJob=null;message("Usage committed; remaining: "+Json.number(Json.object(receipt.get("consumption")),"remaining"));}
 private DesktopDemo(){this(null,true);}
 DesktopDemo(java.util.function.Supplier<Path> outputSelector,boolean visible){chooseOutput=outputSelector!=null?outputSelector:()->{JFileChooser dialog=new JFileChooser();return dialog.showSaveDialog(window)==JFileChooser.APPROVE_OPTION?dialog.getSelectedFile().toPath():null;};window.setDefaultCloseOperation(JFrame.DISPOSE_ON_CLOSE);window.setSize(720,380);JPanel form=new JPanel(new GridLayout(0,1,8,8));form.setBorder(BorderFactory.createEmptyBorder(24,24,24,24));form.add(new JLabel("License key or temporary transfer code"));form.add(key);JButton start=new JButton("Activate / restore saved license");start.addActionListener(e->activate());form.add(start);form.add(metered);JPanel buttons=new JPanel();JButton export=new JButton("Export a report"),retry=new JButton("Retry pending commit"),release=new JButton("Release this device");export.addActionListener(e->export());retry.addActionListener(e->work(()->{commit();return null;}));release.addActionListener(e->work(()->{if(client!=null)client.deactivate();return null;}));JButton pro=new JButton("Pro export");pro.addActionListener(e->work(()->{if(client==null)throw new Exception("Activate first.");client.runFeature("pro",()->Files.writeString(Path.of("pro-report.csv"),"Item,Quantity,Price\nNotebook,2,12.50\n"));return null;}));buttons.add(pro);buttons.add(export);buttons.add(retry);buttons.add(release);JButton refresh=new JButton("Refresh license");refresh.addActionListener(e->work(()->{if(client!=null){client.refreshOnline();message((String)client.status().get("state"));}return null;}));buttons.add(refresh);form.add(buttons);form.add(status);window.add(form);window.addWindowListener(new java.awt.event.WindowAdapter(){public void windowClosed(java.awt.event.WindowEvent e){worker.submit(()->{if(client!=null)client.close();});worker.shutdown();}});window.setVisible(visible);}
 public static void main(String[]args){SwingUtilities.invokeLater(DesktopDemo::new);}
}
