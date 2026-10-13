package licentivo;

import java.util.*;

/** Bounded JSON for the SDK protocol. Rejects duplicate keys, trailing data and deep nesting. */
public final class Json {
    private final String s; private int i;
    private Json(String s) { this.s=s; }
    public static Object parse(String s) { return parse(s,16384); }
    public static Object parse(String s,int limit) { if(s.length()>limit)throw new IllegalArgumentException("JSON size limit");Json p=new Json(s);Object v=p.value(0);p.ws();if(p.i!=s.length())throw p.bad();return v; }
    private IllegalArgumentException bad(){return new IllegalArgumentException("invalid/duplicate JSON");}
    private void ws(){while(i<s.length()&&" \t\r\n".indexOf(s.charAt(i))>=0)i++;}
    private char take(){if(i>=s.length())throw bad();return s.charAt(i++);}
    private Object value(int depth){if(depth>32)throw bad();ws();if(i>=s.length())throw bad();char c=s.charAt(i);
        if(c=='"')return string();
        if(c=='{'){i++;Map<String,Object> m=new LinkedHashMap<>();ws();if(i<s.length()&&s.charAt(i)=='}'){i++;return m;}for(;;){ws();if(i>=s.length()||s.charAt(i)!='"')throw bad();String k=string();if(m.containsKey(k))throw bad();ws();if(take()!=':')throw bad();m.put(k,value(depth+1));ws();char end=take();if(end=='}')return m;if(end!=',')throw bad();}}
        if(c=='['){i++;List<Object> a=new ArrayList<>();ws();if(i<s.length()&&s.charAt(i)==']'){i++;return a;}for(;;){a.add(value(depth+1));ws();char end=take();if(end==']')return a;if(end!=',')throw bad();}}
        for(String token:new String[]{"true","false","null"})if(s.startsWith(token,i)){i+=token.length();return token.equals("null")?null:token.equals("true");}
        int start=i;while(i<s.length()&&"-+0123456789.eE".indexOf(s.charAt(i))>=0)i++;String n=s.substring(start,i);if(!n.matches("-?(0|[1-9][0-9]*)(\\.[0-9]+)?([eE][+-]?[0-9]+)?"))throw bad();try{if(n.indexOf('.')<0&&n.indexOf('e')<0&&n.indexOf('E')<0)return Long.valueOf(n);return new java.math.BigDecimal(n);}catch(NumberFormatException e){throw bad();}
    }
    private String string(){if(take()!='"')throw bad();StringBuilder b=new StringBuilder();while(i<s.length()){char c=take();if(c=='"')return b.toString();if(c<32)throw bad();if(c=='\\'){char e=take();switch(e){case '"':case '\\':case '/':b.append(e);break;case 'b':b.append('\b');break;case 'f':b.append('\f');break;case 'n':b.append('\n');break;case 'r':b.append('\r');break;case 't':b.append('\t');break;case 'u':if(i+4>s.length())throw bad();try{b.append((char)Integer.parseInt(s.substring(i,i+4),16));}catch(NumberFormatException ex){throw bad();}i+=4;break;default:throw bad();}}else b.append(c);}throw bad();}
    @SuppressWarnings("unchecked") public static Map<String,Object> object(Object o){if(!(o instanceof Map))throw new IllegalArgumentException("object required");return (Map<String,Object>)o;}
    public static String text(Map<String,Object> o,String key){Object v=o.get(key);if(!(v instanceof String))throw new IllegalArgumentException("string required: "+key);return (String)v;}
    public static long number(Map<String,Object> o,String key){Object v=o.get(key);if(!(v instanceof Long))throw new IllegalArgumentException("integer required: "+key);return (Long)v;}
    public static String write(Object v){if(v==null)return "null";if(v instanceof String){StringBuilder b=new StringBuilder("\"");for(char c:((String)v).toCharArray()){if(c=='"'||c=='\\')b.append('\\').append(c);else if(c<32)b.append(String.format("\\u%04x",(int)c));else b.append(c);}return b.append('"').toString();}if(v instanceof Boolean||v instanceof Number)return v.toString();if(v instanceof Map){List<String> a=new ArrayList<>();for(Map.Entry<?,?> e:((Map<?,?>)v).entrySet())a.add(write(e.getKey().toString())+":"+write(e.getValue()));return "{"+String.join(",",a)+"}";}if(v instanceof Iterable){List<String>a=new ArrayList<>();for(Object x:(Iterable<?>)v)a.add(write(x));return "["+String.join(",",a)+"]";}throw new IllegalArgumentException("unsupported JSON value");}
}
