package org.serverfleet.mobile;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.Intent;
import android.content.SharedPreferences;
import android.graphics.Color;
import android.graphics.Insets;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.text.InputType;
import android.view.Gravity;
import android.view.View;
import android.view.WindowInsets;
import android.view.inputmethod.InputMethodManager;
import android.webkit.CookieManager;
import android.webkit.WebChromeClient;
import android.webkit.WebResourceError;
import android.webkit.WebResourceRequest;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ProgressBar;
import android.widget.ScrollView;
import android.widget.TextView;
import org.json.JSONObject;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

public final class MainActivity extends Activity {
    private static final int PURPLE = Color.rgb(102, 88, 232);
    private final ExecutorService worker = Executors.newSingleThreadExecutor();
    private LinearLayout root;
    private WebView web;
    private EditText address, username, password;
    private TextView status;
    private Button connect;
    private ProgressBar progress;
    private String origin;
    private boolean disposed;

    @Override public void onCreate(Bundle state) {
        super.onCreate(state);
        root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setBackgroundColor(Color.rgb(245, 246, 251));
        root.setOnApplyWindowInsetsListener((view, insets) -> {
            if (Build.VERSION.SDK_INT >= 30) {
                Insets safe = insets.getInsets(WindowInsets.Type.systemBars() | WindowInsets.Type.ime() | WindowInsets.Type.displayCutout());
                view.setPadding(safe.left, safe.top, safe.right, safe.bottom);
            } else {
                view.setPadding(insets.getSystemWindowInsetLeft(), insets.getSystemWindowInsetTop(),
                        insets.getSystemWindowInsetRight(), insets.getSystemWindowInsetBottom());
            }
            return insets;
        });
        setContentView(root);
        root.requestApplyInsets();
        if (Build.VERSION.SDK_INT >= 33) getOnBackInvokedDispatcher().registerOnBackInvokedCallback(0, this::goBack);
        showLogin();
    }
    private int dp(int value) { return Math.round(value * getResources().getDisplayMetrics().density); }
    private TextView text(String value, int size, int color) {
        TextView view = new TextView(this);
        view.setText(value); view.setTextSize(size); view.setTextColor(color); return view;
    }
    private void space(LinearLayout parent, int height) { parent.addView(new View(this), new LinearLayout.LayoutParams(1, dp(height))); }
    private EditText field(LinearLayout form, String label, String hint, int type) {
        form.addView(text(label, 13, Color.rgb(80, 86, 105)));
        EditText input = new EditText(this); input.setSingleLine(true); input.setTextSize(16);
        input.setInputType(type); input.setHint(hint); input.setPadding(dp(14), dp(10), dp(14), dp(10));
        GradientDrawable shape = new GradientDrawable(); shape.setColor(Color.WHITE); shape.setCornerRadius(dp(12));
        shape.setStroke(dp(1), Color.rgb(220, 224, 237)); input.setBackground(shape);
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(-1, dp(52)); params.topMargin = dp(8);
        form.addView(input, params); space(form, 20); return input;
    }
    private SharedPreferences preferences() { return getSharedPreferences("serverfleet-entry", MODE_PRIVATE); }
    private void showLogin() {
        if (web != null) { web.stopLoading(); web.destroy(); web = null; }
        root.removeAllViews();
        ScrollView scroll = new ScrollView(this); scroll.setFillViewport(true);
        LinearLayout form = new LinearLayout(this); form.setOrientation(LinearLayout.VERTICAL);
        form.setPadding(dp(28), dp(52), dp(28), dp(28));
        TextView logo = text("SF", 27, Color.WHITE); logo.setGravity(Gravity.CENTER);
        GradientDrawable badge = new GradientDrawable(); badge.setColor(PURPLE); badge.setCornerRadius(dp(20)); logo.setBackground(badge);
        form.addView(logo, new LinearLayout.LayoutParams(dp(74), dp(74))); space(form, 24);
        TextView title = text("连接你的 Server Fleet", 26, Color.rgb(32, 37, 52));
        title.setTypeface(null, Typeface.BOLD); form.addView(title); space(form, 10);
        form.addView(text("掌握节点状态，随时使用 AI 运维。", 14, Color.rgb(122, 129, 147))); space(form, 34);
        address = field(form, "控制台网址", "https://agent.wentian.org:8443", InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
        address.setText(preferences().getString("origin", ""));
        username = field(form, "管理员账号", "请输入账号", InputType.TYPE_CLASS_TEXT);
        username.setText(preferences().getString("username", ""));
        password = field(form, "密码", "请输入密码", InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_PASSWORD);
        connect = new Button(this); connect.setText("连接并登录"); connect.setTextColor(Color.WHITE); connect.setTextSize(16);
        connect.setBackgroundTintList(android.content.res.ColorStateList.valueOf(PURPLE));
        form.addView(connect, new LinearLayout.LayoutParams(-1, dp(54)));
        status = text("网址与账号会保留；密码每次填写。", 12, Color.rgb(122, 129, 147));
        status.setPadding(0, dp(18), 0, 0); form.addView(status);
        connect.setOnClickListener(view -> login());
        password.setOnEditorActionListener((view, action, event) -> { login(); return true; });
        scroll.addView(form); root.addView(scroll, new LinearLayout.LayoutParams(-1, -1));
    }
    private void login() {
        if (!connect.isEnabled()) return;
        final String server, account = username.getText().toString().trim(), secret = password.getText().toString();
        try { server = ServerAddress.normalize(address.getText().toString()); }
        catch (IllegalArgumentException e) { status.setText(e.getMessage()); return; }
        if (account.isEmpty() || secret.isEmpty()) { status.setText("请填写管理员账号和密码。"); return; }
        connect.setEnabled(false); connect.setText("正在连接…"); status.setText("正在验证网址与登录信息…");
        ((InputMethodManager) getSystemService(INPUT_METHOD_SERVICE)).hideSoftInputFromWindow(password.getWindowToken(), 0);
        final String existingCookies = CookieManager.getInstance().getCookie(server);
        worker.execute(() -> {
            try {
                Reply home = request(server + "/", null, null, server);
                if (home.code != 200 || !home.body.contains("mobile-app-v1"))
                    throw new Exception("这个控制台尚未提供 App 页面，请先更新 Server Fleet 服务端。");
                JSONObject body = new JSONObject(); body.put("username", account); body.put("password", secret);
                Reply reply = request(server + "/api/v1/auth/login", body.toString(), existingCookies, server);
                if (reply.code != 200) throw new Exception(loginError(reply.code));
                JSONObject payload = new JSONObject(reply.body);
                if (!payload.has("identity") || reply.cookies.isEmpty())
                    throw new Exception("登录响应不完整，请确认这是 Server Fleet 控制台网址。");
                runOnUiThread(() -> {
                    if (disposed) return;
                    preferences().edit().putString("origin", server).putString("username", account).apply();
                    password.setText(""); origin = server; installCookies(reply.cookies, 0);
                });
            } catch (Exception e) {
                final String message = e instanceof java.net.SocketTimeoutException ? "连接超时，请检查网址或网络后重试。" :
                    e instanceof javax.net.ssl.SSLException ? "无法验证网站证书，请检查网址和服务器证书。" :
                    e.getMessage() == null ? "无法连接，请检查网址或网络。" : e.getMessage();
                runOnUiThread(() -> { if (!disposed) { connect.setEnabled(true); connect.setText("连接并登录"); status.setText(message); } });
            }
        });
    }
    private static String loginError(int code) {
        if (code == 401 || code == 400) return "账号或密码不正确，请重新填写。";
        if (code == 429) return "登录尝试过于频繁，请稍后再试。";
        if (code == 403) return "服务端拒绝登录，请检查账号状态和控制台网址。";
        return "登录暂时不可用（HTTP " + code + "），请稍后重试。";
    }
    private void installCookies(List<String> cookies, int index) {
        if (disposed) return;
        if (index == cookies.size()) { CookieManager.getInstance().flush(); showConsole(); return; }
        CookieManager manager = CookieManager.getInstance(); manager.setAcceptCookie(true);
        manager.setCookie(origin, cookies.get(index), accepted -> {
            if (Boolean.TRUE.equals(accepted)) installCookies(cookies, index + 1);
            else { connect.setEnabled(true); connect.setText("连接并登录"); status.setText("无法保存登录会话，请检查网址后重试。"); }
        });
    }
    private void showConsole() {
        root.removeAllViews();
        LinearLayout toolbar = new LinearLayout(this); toolbar.setGravity(Gravity.CENTER_VERTICAL);
        toolbar.setPadding(dp(12), 0, dp(6), 0); toolbar.setBackgroundColor(Color.WHITE);
        TextView site = text(Uri.parse(origin).getAuthority(), 12, Color.rgb(122, 129, 147));
        site.setSingleLine(true); toolbar.addView(site, new LinearLayout.LayoutParams(0, -2, 1));
        Button menu = new Button(this); menu.setText("···"); menu.setContentDescription("连接选项");
        menu.setOnClickListener(view -> new AlertDialog.Builder(this).setTitle("连接选项")
            .setItems(new String[]{"刷新当前页面", "返回节点", "切换网址或账号"}, (dialog, which) -> {
                if (which == 0) web.reload();
                else if (which == 1) web.loadUrl(origin + "/#/app/nodes");
                else showLogin();
            }).show());
        toolbar.addView(menu, new LinearLayout.LayoutParams(dp(56), dp(40)));
        root.addView(toolbar, new LinearLayout.LayoutParams(-1, dp(40)));
        progress = new ProgressBar(this, null, android.R.attr.progressBarStyleHorizontal);
        root.addView(progress, new LinearLayout.LayoutParams(-1, dp(2)));
        web = new WebView(this); web.getSettings().setJavaScriptEnabled(true); web.getSettings().setDomStorageEnabled(true);
        web.getSettings().setUserAgentString(web.getSettings().getUserAgentString() + " ServerFleetMobile/0.1.0");
        web.setWebChromeClient(new WebChromeClient() {
            @Override public void onProgressChanged(WebView view, int value) { progress.setProgress(value); progress.setVisibility(value == 100 ? View.GONE : View.VISIBLE); }
        });
        web.setWebViewClient(new WebViewClient() {
            @Override public boolean shouldOverrideUrlLoading(WebView view, WebResourceRequest req) {
                String target = req.getUrl().toString();
                if (ServerAddress.sameOrigin(origin, target)) return false;
                if (req.isForMainFrame() && ("https".equals(req.getUrl().getScheme()) || "http".equals(req.getUrl().getScheme()))) {
                    try { startActivity(new Intent(Intent.ACTION_VIEW, req.getUrl())); } catch (Exception ignored) {}
                }
                return true;
            }
            @Override public void onReceivedError(WebView view, WebResourceRequest req, WebResourceError error) {
                if (req.isForMainFrame()) new AlertDialog.Builder(MainActivity.this).setTitle("连接中断")
                    .setMessage("请检查网络，恢复后点击重试。").setPositiveButton("重试", (dialog, which) -> view.loadUrl(origin + "/#/app/nodes"))
                    .setNegativeButton("连接设置", (dialog, which) -> showLogin()).show();
            }
        });
        root.addView(web, new LinearLayout.LayoutParams(-1, 0, 1)); web.loadUrl(origin + "/#/app/nodes");
    }
    private static final class Reply {
        int code; String body; List<String> cookies = new ArrayList<>();
    }
    private static Reply request(String url, String json, String cookie, String origin) throws Exception {
        HttpURLConnection conn = (HttpURLConnection) new URL(url).openConnection();
        conn.setConnectTimeout(15000); conn.setReadTimeout(30000); conn.setInstanceFollowRedirects(false);
        conn.setRequestProperty("Accept", "application/json"); conn.setRequestProperty("Origin", origin);
        conn.setRequestProperty("User-Agent", "ServerFleetMobile/0.1.0");
        if (cookie != null && !cookie.isEmpty()) conn.setRequestProperty("Cookie", cookie);
        try {
            if (json != null) {
                conn.setRequestMethod("POST"); conn.setDoOutput(true); conn.setRequestProperty("Content-Type", "application/json");
                conn.setRequestProperty("X-Requested-With", "ServerFleetWeb");
                try (java.io.OutputStream stream = conn.getOutputStream()) { stream.write(json.getBytes(StandardCharsets.UTF_8)); }
            }
            Reply reply = new Reply(); reply.code = conn.getResponseCode();
            for (Map.Entry<String, List<String>> entry : conn.getHeaderFields().entrySet())
                if ("Set-Cookie".equalsIgnoreCase(entry.getKey())) reply.cookies.addAll(entry.getValue());
            InputStream response = reply.code >= 400 ? conn.getErrorStream() : conn.getInputStream();
            if (response == null) reply.body = "";
            else try (InputStream stream = response) {
                java.io.ByteArrayOutputStream buffer = new java.io.ByteArrayOutputStream();
                byte[] chunk = new byte[8192]; int count;
                while ((count = stream.read(chunk)) != -1) {
                    if (buffer.size() + count > 1048576) throw new Exception("服务器返回内容过大，请检查网址。");
                    buffer.write(chunk, 0, count);
                }
                reply.body = buffer.toString(StandardCharsets.UTF_8.name());
            }
            return reply;
        } finally { conn.disconnect(); }
    }
    private void goBack() {
        if (web != null && web.canGoBack()) web.goBack();
        else if (web != null) showLogin(); else finish();
    }
    // API 33+ uses the platform dispatcher registered in onCreate. This override
    // handles API 29-32 only; no AndroidX dependency is needed for the fallback.
    @android.annotation.SuppressLint("GestureBackNavigation")
    @Override public void onBackPressed() { if (Build.VERSION.SDK_INT < 33) goBack(); }
    @Override protected void onPause() { if (web != null) web.onPause(); super.onPause(); }
    @Override protected void onResume() { super.onResume(); if (web != null) web.onResume(); }
    @Override protected void onDestroy() {
        disposed = true; worker.shutdownNow();
        if (web != null) { web.stopLoading(); web.destroy(); }
        super.onDestroy();
    }
}
