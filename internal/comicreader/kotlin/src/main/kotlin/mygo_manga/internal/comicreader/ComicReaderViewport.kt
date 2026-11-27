package mygo_manga.internal.comicreader

import android.content.Context
import android.view.MotionEvent
import android.view.ScaleGestureDetector
import android.view.ViewConfiguration
import android.view.ViewGroup
import androidx.recyclerview.widget.RecyclerView
import kotlin.math.abs
import kotlin.math.ceil
import kotlin.math.roundToInt

internal class ComicReaderViewport(
    context: Context,
    private val pages: ComicReaderView = ComicReaderView(context)
) : ViewGroup(context), Reader by pages {
    private data class Anchor(val position: Int, val top: Float)

    private var zoom = 1f
    private var anchor: Anchor? = null
    private var focusX = 0f
    private var focusY = 0f
    private val touchSlop = ViewConfiguration.get(context).scaledTouchSlop
    private var touchX = 0f
    private var touchY = 0f
    private var ownsGesture = false
    private var scrollRemainder = 0f

    private val pinch = ScaleGestureDetector(context, object : ScaleGestureDetector.SimpleOnScaleGestureListener() {
        override fun onScaleBegin(detector: ScaleGestureDetector): Boolean {
            focusX = detector.focusX
            focusY = detector.focusY
            return true
        }

        override fun onScale(detector: ScaleGestureDetector): Boolean {
            val nextZoom = (zoom * detector.scaleFactor).coerceIn(1f, 4f)
            val shiftY = detector.focusY / nextZoom - focusY / zoom
            if (nextZoom != zoom || (nextZoom > 1f && shiftY != 0f)) {
                val currentAnchor = anchor ?: pages.layout.findFirstVisibleItemPosition()
                    .takeIf { it != RecyclerView.NO_POSITION }
                    ?.let { position -> pages.layout.findViewByPosition(position)?.let { Anchor(position, it.top.toFloat()) } }
                anchor = currentAnchor?.copy(top = currentAnchor.top + shiftY)
                requestLayout()
            }
            val offset = detector.focusX - (focusX - pages.translationX) * nextZoom / zoom
            zoom = nextZoom
            pages.scaleX = zoom
            pages.scaleY = zoom
            panTo(offset)
            focusX = detector.focusX
            focusY = detector.focusY
            return true
        }
    }).apply {
        isQuickScaleEnabled = false
        isStylusScaleEnabled = false
    }

    init {
        pages.pivotX = 0f
        pages.pivotY = 0f
        addView(pages, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT))
    }

    override fun open(
        pages: List<String>, firstPage: Long, hasNext: Boolean, dark: Boolean,
        loader: PageLoader, onPage: (Long) -> Unit, onTap: () -> Unit, onEnd: () -> Unit
    ): AutoCloseable {
        anchor = null
        zoom = 1f
        this.pages.scaleX = 1f
        this.pages.scaleY = 1f
        panTo(0f)
        requestLayout()
        return this.pages.open(pages, firstPage, hasNext, dark, loader, onPage, onTap, onEnd)
    }

    override fun seek(page: Long) {
        anchor = null
        pages.seek(page)
    }

    override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
        val viewWidth = MeasureSpec.getSize(widthMeasureSpec)
        val viewHeight = MeasureSpec.getSize(heightMeasureSpec)
        setMeasuredDimension(viewWidth, viewHeight)
        // Measure the unscaled viewport so RecyclerView still lays out only the visible area.
        pages.measure(
            MeasureSpec.makeMeasureSpec(viewWidth, MeasureSpec.EXACTLY),
            MeasureSpec.makeMeasureSpec(ceil(viewHeight / zoom).toInt(), MeasureSpec.EXACTLY)
        )
    }

    override fun onLayout(changed: Boolean, left: Int, top: Int, right: Int, bottom: Int) {
        anchor?.let { pages.layout.scrollToPositionWithOffset(it.position, it.top.roundToInt()) }
        anchor = null
        pages.layout(0, 0, pages.measuredWidth, pages.measuredHeight)
        panTo(pages.translationX)
    }

    override fun dispatchTouchEvent(event: MotionEvent): Boolean {
        when (event.actionMasked) {
            MotionEvent.ACTION_DOWN -> {
                ownsGesture = false
                scrollRemainder = 0f
                touchX = event.x
                touchY = event.y
            }
            MotionEvent.ACTION_POINTER_DOWN -> takeGesture(event)
            MotionEvent.ACTION_POINTER_UP -> if (event.pointerCount == 2) {
                val remaining = 1 - event.actionIndex
                touchX = event.getX(remaining)
                touchY = event.getY(remaining)
            }
            MotionEvent.ACTION_MOVE -> if (event.pointerCount == 1 && zoom > 1f) {
                val dx = event.x - touchX
                val dy = event.y - touchY
                if (!ownsGesture && abs(dx) > touchSlop && abs(dx) > abs(dy)) takeGesture(event)
                if (ownsGesture) {
                    panTo(pages.translationX + dx)
                    scrollRemainder -= dy / zoom
                    val scroll = scrollRemainder.toInt()
                    if (scroll != 0) {
                        pages.scrollBy(0, scroll)
                        scrollRemainder -= scroll
                    }
                    touchX = event.x
                    touchY = event.y
                }
            }
        }
        pinch.onTouchEvent(event)
        return ownsGesture || super.dispatchTouchEvent(event)
    }

    private fun takeGesture(event: MotionEvent) {
        if (ownsGesture) return
        val cancel = MotionEvent.obtain(event)
        cancel.action = MotionEvent.ACTION_CANCEL
        super.dispatchTouchEvent(cancel)
        cancel.recycle()
        pages.stopScroll()
        ownsGesture = true
    }

    private fun panTo(offset: Float) {
        pages.translationX = offset.coerceIn(width * (1f - zoom), 0f)
    }
}
