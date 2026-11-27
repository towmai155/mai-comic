package mygo_manga.internal.comicreader

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Color
import android.provider.Settings
import android.util.LruCache
import android.view.GestureDetector
import android.view.Gravity
import android.view.MotionEvent
import android.view.View
import android.view.ViewGroup
import android.widget.FrameLayout
import android.widget.ImageView
import android.widget.TextView
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.RecyclerView
import dev.mygo.Native
import dev.mygo.NativeCloser
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlin.math.roundToInt

fun makeReader(): Reader = ComicReaderViewport(Native.context)

internal class ComicReaderView(context: Context) : RecyclerView(context), Reader {
    private data class Picture(val bitmap: Bitmap, val ratio: Float)

    private class Chapter(
        val pages: List<String>,
        val hasNext: Boolean,
        val loader: PageLoader,
        val onPage: (Long) -> Unit,
        val onTap: () -> Unit,
        val onEnd: () -> Unit
    ) {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
        val ratios = FloatArray(pages.size) { 1.4f }
        var first = -1
        var lastReady = false
        var pendingEnd = false
        var ended = false
    }

    val layout = LinearLayoutManager(context)
    private val decode = Dispatchers.IO.limitedParallelism(3)
    private val pictures = object : LruCache<String, Picture>(40 * 1024 * 1024) {
        override fun sizeOf(key: String, value: Picture): Int = value.bitmap.allocationByteCount
    }
    private val pageAdapter = PageAdapter()
    private var chapter: Chapter? = null
    private var darkMode = false

    init {
        layoutManager = layout
        adapter = pageAdapter
        itemAnimator = null
        setHasFixedSize(true)
        setItemViewCacheSize(3)
        isVerticalScrollBarEnabled = true

        val taps = GestureDetector(context, object : GestureDetector.SimpleOnGestureListener() {
            override fun onDown(event: MotionEvent) = true
            override fun onSingleTapUp(event: MotionEvent): Boolean {
                val holder = findChildViewUnder(event.x, event.y)?.let(::getChildViewHolder) as? PageHolder
                if (holder?.status?.isClickable != true) chapter?.onTap?.invoke()
                return true
            }
        })
        addOnItemTouchListener(object : SimpleOnItemTouchListener() {
            override fun onInterceptTouchEvent(rv: RecyclerView, event: MotionEvent): Boolean {
                taps.onTouchEvent(event)
                return false
            }
        })
        addOnScrollListener(object : OnScrollListener() {
            override fun onScrolled(rv: RecyclerView, dx: Int, dy: Int) {
                val current = chapter ?: return
                val first = layout.findFirstVisibleItemPosition()
                if (first >= 0 && current.pages.isNotEmpty()) {
                    val page = first.coerceAtMost(current.pages.lastIndex)
                    if (page != current.first) {
                        current.first = page
                        current.onPage(page.toLong())
                    }
                }
                if (layout.findLastVisibleItemPosition() < current.pages.size) {
                    current.pendingEnd = false
                } else if (dy > 0 && scrollState != SCROLL_STATE_IDLE) {
                    current.pendingEnd = true
                    maybeAdvance(current)
                }
            }
        })
    }

    override fun open(
        pages: List<String>, firstPage: Long, hasNext: Boolean, dark: Boolean,
        loader: PageLoader, onPage: (Long) -> Unit, onTap: () -> Unit, onEnd: () -> Unit
    ): AutoCloseable {
        stopScroll()
        chapter?.scope?.cancel()
        val current = Chapter(pages, hasNext, loader, onPage, onTap, onEnd)
        chapter = current
        setDark(dark)
        val restoreRefreshRate = preferReaderRefresh()
        pageAdapter.notifyDataSetChanged()
        seek(firstPage)
        return NativeCloser {
            current.scope.cancel()
            restoreRefreshRate()
            if (chapter === current) {
                stopScroll()
                chapter = null
                pageAdapter.notifyDataSetChanged()
            }
        }
    }

    override fun seek(page: Long) {
        val current = chapter ?: return
        if (current.pages.isEmpty()) return
        stopScroll()
        current.pendingEnd = false
        layout.scrollToPositionWithOffset(page.coerceIn(0, current.pages.lastIndex.toLong()).toInt(), 0)
    }

    override fun setDark(dark: Boolean) {
        darkMode = dark
        setBackgroundColor(if (dark) Color.rgb(20, 21, 24) else Color.rgb(249, 249, 251))
        for (index in 0 until childCount) {
            (getChildViewHolder(getChildAt(index)) as? PageHolder)?.updateColors()
        }
    }

    private fun maybeAdvance(current: Chapter) {
        if (chapter === current && current.hasNext && current.pendingEnd && current.lastReady && !current.ended) {
            current.pendingEnd = false
            current.ended = true
            current.onEnd()
        }
    }

    private fun pageWidth(): Int = width.takeIf { it > 0 } ?: resources.displayMetrics.widthPixels
    private fun dp(value: Int): Int = (value * resources.displayMetrics.density).roundToInt()

    private fun preferReaderRefresh(): () -> Unit {
        val window = Native.activity?.window ?: return {}
        val display = window.decorView.display ?: return {}
        val mode = display.mode
        val limit = Settings.System.getFloat(context.contentResolver, "peak_refresh_rate", 0f)
        val preferred = display.supportedModes
            .filter { it.physicalWidth == mode.physicalWidth && it.physicalHeight == mode.physicalHeight }
            .map { it.refreshRate }
            .filter { limit <= 0f || it <= limit + 0.5f }
            .maxOrNull() ?: return {}
        val attrs = window.attributes
        val previous = attrs.preferredRefreshRate
        if (preferred <= 60.5f || preferred == previous || attrs.preferredDisplayModeId != 0) return {}
        attrs.preferredRefreshRate = preferred
        window.attributes = attrs
        return {
            val current = window.attributes
            if (current.preferredRefreshRate == preferred) {
                current.preferredRefreshRate = previous
                window.attributes = current
            }
        }
    }

    private fun decodePage(bytes: ByteArray, targetWidth: Int): Picture {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeByteArray(bytes, 0, bytes.size, bounds)
        val w = bounds.outWidth
        val h = bounds.outHeight
        require(w > 0 && h > 0 && w.toLong() * h <= 64L * 1024 * 1024)
        var sample = 1
        while (w / sample > targetWidth * 2 || h / sample > 4096) sample *= 2
        val scaled = BitmapFactory.Options().apply { inSampleSize = sample }
        val bitmap = requireNotNull(BitmapFactory.decodeByteArray(bytes, 0, bytes.size, scaled))
        return Picture(bitmap, h.toFloat() / w)
    }

    inner class PageHolder : ViewHolder(FrameLayout(context)) {
        private val box = itemView as FrameLayout
        val image = ImageView(context).apply { scaleType = ImageView.ScaleType.FIT_XY }
        val status = TextView(context).apply {
            gravity = Gravity.CENTER
            textSize = 15f
        }
        var job: Job? = null

        init {
            box.addView(image, FrameLayout.LayoutParams(-1, -2, Gravity.TOP))
            box.addView(status, FrameLayout.LayoutParams(-1, -1))
        }

        fun updateColors() {
            status.setTextColor(if (darkMode) Color.rgb(218, 219, 223) else Color.rgb(64, 66, 72))
        }

        fun size(imageHeight: Int, rowHeight: Int) {
            val margin = dp(8)
            val row = box.layoutParams as? LayoutParams
            if (row == null) {
                box.layoutParams = LayoutParams(-1, rowHeight).apply { bottomMargin = margin }
            } else if (row.height != rowHeight || row.bottomMargin != margin) {
                // RecyclerView stores this row's ViewHolder in its LayoutParams.
                row.height = rowHeight
                row.bottomMargin = margin
                box.requestLayout()
            }
            val picture = image.layoutParams
            if (picture.height != imageHeight) {
                picture.height = imageHeight
                image.requestLayout()
            }
        }

        fun clear() {
            job?.cancel()
            job = null
            image.setImageDrawable(null)
            image.contentDescription = null
            status.setOnClickListener(null)
            status.isClickable = false
            status.visibility = View.VISIBLE
        }
    }

    private inner class PageAdapter : Adapter<PageHolder>() {
        override fun getItemCount(): Int = chapter?.let { it.pages.size + if (it.hasNext) 1 else 0 } ?: 0
        override fun onCreateViewHolder(parent: ViewGroup, viewType: Int) = PageHolder()
        override fun onViewRecycled(holder: PageHolder) { holder.clear() }

        override fun onBindViewHolder(holder: PageHolder, position: Int) {
            val current = chapter ?: return
            holder.clear()
            holder.updateColors()
            if (position == current.pages.size) {
                holder.size(dp(56), dp(56))
                holder.status.text = "打开下一章"
                holder.status.setOnClickListener { current.onEnd() }
                return
            }

            val finalPage = position == current.pages.lastIndex
            val viewWidth = pageWidth()
            fun size(ratio: Float) {
                val imageHeight = (viewWidth * ratio).roundToInt().coerceAtLeast(1)
                val rowHeight = if (finalPage && (current.hasNext || current.pages.size > 1)) {
                    imageHeight.coerceAtLeast((parent as View).measuredHeight)
                } else imageHeight
                holder.size(imageHeight, rowHeight)
            }
            fun show(picture: Picture) {
                current.ratios[position] = picture.ratio
                size(picture.ratio)
                holder.image.setImageBitmap(picture.bitmap)
                holder.image.contentDescription = "第 ${position + 1} 页"
                holder.status.visibility = View.GONE
                if (finalPage) {
                    current.lastReady = true
                    post { maybeAdvance(current) }
                }
            }

            val address = current.pages[position]
            pictures.get(address)?.let {
                show(it)
                return
            }
            size(current.ratios[position])
            holder.status.text = "正在加载第 ${position + 1} 页"
            holder.job = current.scope.launch {
                try {
                    val picture = withContext(decode) { decodePage(current.loader.load(address), viewWidth) }
                    if (chapter !== current) return@launch
                    pictures.put(address, picture)
                    show(picture)
                } catch (cancelled: CancellationException) {
                    throw cancelled
                } catch (_: Exception) {
                    holder.status.text = "第 ${position + 1} 页加载失败 · 点击重试"
                    holder.status.setOnClickListener {
                        val index = holder.bindingAdapterPosition
                        if (index != NO_POSITION && chapter === current) notifyItemChanged(index)
                    }
                }
            }
        }
    }
}
